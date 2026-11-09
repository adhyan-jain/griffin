package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hostagent"
	"github.com/griffin/core/internal/vault"
)

// The laptop's real upload client against the real handler over HTTP: the multipart format, the owner
// the host acts for, the encryption rule, duplicates and the missed-document loop.
func TestHostVaultEndToEnd(t *testing.T) {
	h, store, _ := newVault()
	owner := uuid.New()
	host := &domain.Host{ID: uuid.New(), UserID: owner, Kind: domain.HostKindLaptop, Capabilities: []string{"files"}, Advertised: []string{"files"}}
	mux := http.NewServeMux()
	withHost := func(fn http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer ghk_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fn(w, r.WithContext(middleware.WithHost(r.Context(), host)))
		})
	}
	mux.Handle("POST /api/v1/host/vault", withHost(h.Add))
	mux.Handle("GET /api/v1/host/vault", withHost(h.List))
	mux.Handle("GET /api/v1/host/vault/misses", withHost(h.ListMisses))
	mux.Handle("DELETE /api/v1/host/vault/misses/{id}", withHost(h.ResolveMiss))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := hostagent.NewClient(srv.URL, "ghk_test", nil)
	ctx := context.Background()

	// the owner asked for a document that was not there (recorded as the app would)
	missReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"PAN card image"}`)).WithContext(asUser(owner))
	h.AddMiss(httptest.NewRecorder(), missReq)

	// the laptop has the files
	dir := t.TempDir()
	scan := func() []vault.Candidate {
		c, err := vault.Scan(ctx, []string{dir}, 4)
		require.NoError(t, err)
		return c
	}
	secret := []byte("PAN ABCDE1234F")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "PAN_card.jpg"), secret, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AJ_resume_context.md"), []byte("# Career"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "holiday.jpg"), []byte("not a document"), 0o600))
	cands := scan()
	require.Len(t, cands, 2)

	// answering the miss: it is matched to the right file and uploaded encrypted
	misses, err := client.VaultMisses(ctx)
	require.NoError(t, err)
	require.Len(t, misses, 1)
	match := vault.MatchMiss(misses[0].Query, cands)
	require.NotNil(t, match)
	assert.Equal(t, "PAN_card.jpg", match.Name)

	up := hostUploaderForTest{client}
	out, err := vault.Mirror(ctx, up, "laptop passphrase", *match)
	require.NoError(t, err)
	assert.True(t, out.Uploaded && out.Encrypted)
	require.NoError(t, client.ResolveVaultMiss(ctx, misses[0].ID))
	left, _ := client.VaultMisses(ctx)
	assert.Empty(t, left, "the miss is cleared once the document is added")

	// what the cloud holds is ciphertext, attributed to the laptop and to the right owner
	require.Len(t, store.arts, 1)
	stored := store.bodies[store.arts[0].ID]
	assert.True(t, vault.IsEncrypted(stored))
	assert.False(t, bytes.Contains(stored, []byte("ABCDE1234F")))
	assert.Equal(t, owner, store.arts[0].UserID)
	plain, err := vault.Decrypt("laptop passphrase", stored)
	require.NoError(t, err)
	assert.Equal(t, secret, plain, "the owner can read it back with their passphrase")

	// an ordinary document, and then the same one again
	var resume vault.Candidate
	for _, c := range cands {
		if c.Name == "AJ_resume_context.md" {
			resume = c
		}
	}
	out, err = vault.Mirror(ctx, up, "", resume)
	require.NoError(t, err)
	assert.True(t, out.Uploaded && !out.Encrypted)
	out, err = vault.Mirror(ctx, up, "", resume)
	require.NoError(t, err)
	assert.True(t, out.Duplicate && !out.Uploaded, "mirroring again does not upload twice")
	assert.Equal(t, 2, store.saves)

	// a host that was never given files is refused by the server even if its own code is wrong
	host.Capabilities = []string{"agent"}
	_, err = client.VaultAdd(ctx, vault.Upload{Label: "x", Category: domain.VaultOther, Filename: "x.txt", Data: []byte("x")})
	require.Error(t, err)
	var apiErr *hostagent.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.Status)

	// and a wrong token never reaches the handler
	_, err = hostagent.NewClient(srv.URL, "wrong", nil).VaultEntries(ctx)
	assert.ErrorIs(t, err, hostagent.ErrUnauthorized)
}

type hostUploaderForTest struct{ c *hostagent.Client }

func (u hostUploaderForTest) Add(ctx context.Context, up vault.Upload) (bool, error) {
	return u.c.VaultAdd(ctx, up)
}
