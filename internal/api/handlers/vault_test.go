package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
	"github.com/griffin/core/internal/vault"
)

type fakeVaultStore struct {
	mu     sync.Mutex
	arts   []*domain.Artifact
	bodies map[uuid.UUID][]byte
	saves  int
	err    error
}

func (f *fakeVaultStore) SaveArtifact(_ context.Context, in storage.SaveArtifactInput) (*domain.Artifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	b, _ := io.ReadAll(in.Body)
	size := int64(len(b))
	name := in.Filename
	a := &domain.Artifact{ID: uuid.New(), UserID: in.UserID, Title: in.Title, Filename: &name, FileSizeBytes: &size, Metadata: in.Metadata, CreatedAt: time.Now()}
	if f.bodies == nil {
		f.bodies = map[uuid.UUID][]byte{}
	}
	f.bodies[a.ID] = b
	f.arts = append(f.arts, a)
	f.saves++
	return a, nil
}

func (f *fakeVaultStore) ListByKind(_ context.Context, userID uuid.UUID, kind string) ([]*domain.Artifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Artifact
	for _, a := range f.arts {
		var m struct{ Kind string }
		if a.UserID == userID && json.Unmarshal(a.Metadata, &m) == nil && m.Kind == kind {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeMissStore struct {
	raw []byte
	err error
}

func (f *fakeMissStore) VaultMisses(context.Context, uuid.UUID) ([]byte, error) { return f.raw, f.err }
func (f *fakeMissStore) SetVaultMisses(_ context.Context, _ uuid.UUID, m []byte) error {
	f.raw = m
	return f.err
}

type form struct {
	fields   map[string]string
	filename string
	content  []byte
	ctype    string
}

func upload(t *testing.T, h *VaultHandler, ctx context.Context, f form) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range f.fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	if f.content != nil {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+f.filename+`"`)
		if f.ctype != "" {
			hdr.Set("Content-Type", f.ctype)
		}
		part, err := mw.CreatePart(hdr)
		require.NoError(t, err)
		part.Write(f.content)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vault", &body).WithContext(ctx)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.Add(rec, req)
	var env struct{ Data map[string]any }
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Code, env.Data
}

func asUser(u uuid.UUID) context.Context {
	return context.WithValue(context.Background(), middleware.UserIDKey, u)
}

func newVault() (*VaultHandler, *fakeVaultStore, *fakeMissStore) {
	s, m := &fakeVaultStore{}, &fakeMissStore{}
	return NewVaultHandler(s, m, 0), s, m
}

func TestVault_SensitiveDocumentsMustArriveEncrypted(t *testing.T) {
	h, store, _ := newVault()
	user := uuid.New()
	plain := []byte("\x89PNG pretend this is a scan of a PAN card")

	for _, cat := range []string{"id", "finance", "ID"} {
		code, _ := upload(t, h, asUser(user), form{fields: map[string]string{"label": "PAN card", "category": cat}, filename: "pan.png", content: plain, ctype: "image/png"})
		assert.Equal(t, http.StatusBadRequest, code, "plain %s document must be refused", cat)
	}
	assert.Zero(t, store.saves, "a refused document must not reach storage at all")

	enc, err := vault.Encrypt("passphrase", plain)
	require.NoError(t, err)
	code, data := upload(t, h, asUser(user), form{fields: map[string]string{"label": "PAN card", "category": "id"}, filename: "pan.png", content: enc})
	require.Equal(t, http.StatusCreated, code)
	entry := data["entry"].(map[string]any)
	assert.Equal(t, true, entry["sensitive"])
	assert.Equal(t, true, entry["encrypted"])
	assert.Equal(t, "pan.png.gvlt", entry["filename"], "ciphertext gets a .gvlt name so nobody opens it by mistake")
	assert.False(t, bytes.Contains(store.bodies[store.arts[0].ID], []byte("PAN card")), "storage holds ciphertext only")
	assert.True(t, vault.IsEncrypted(store.bodies[store.arts[0].ID]))

	// a file that merely claims to be encrypted is not accepted as such: the format is checked
	code, _ = upload(t, h, asUser(user), form{fields: map[string]string{"label": "Aadhaar", "category": "id"}, filename: "a.png", content: []byte("GVLT1 but not really a vault file at all")})
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestVault_OrdinaryDocumentsMayBePlainOrEncrypted(t *testing.T) {
	h, _, _ := newVault()
	user := uuid.New()
	code, data := upload(t, h, asUser(user), form{fields: map[string]string{"label": "  AJ   resume context ", "category": "resume"}, filename: "../../AJ_resume_context.md", content: []byte("# Career"), ctype: "text/markdown"})
	require.Equal(t, http.StatusCreated, code)
	e := data["entry"].(map[string]any)
	assert.Equal(t, "AJ resume context", e["label"])
	assert.Equal(t, "AJ_resume_context.md", e["filename"], "a path in the file name must be dropped")
	assert.Equal(t, false, e["sensitive"])
	assert.Equal(t, false, e["encrypted"])
	assert.Equal(t, "web", e["source"])

	code, data = upload(t, h, asUser(user), form{fields: map[string]string{"label": "Cert"}, filename: "c.pdf", content: []byte("%PDF-1.4 ...")})
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "other", data["entry"].(map[string]any)["category"], "no category means other")
}

func TestVault_ValidationAndLimits(t *testing.T) {
	h, _, _ := newVault()
	ctx := asUser(uuid.New())
	cases := map[string]form{
		"no label":         {fields: map[string]string{"category": "other"}, filename: "a", content: []byte("x")},
		"bad category":     {fields: map[string]string{"label": "x", "category": "secrets"}, filename: "a", content: []byte("x")},
		"no file":          {fields: map[string]string{"label": "x"}},
		"empty file":       {fields: map[string]string{"label": "x"}, filename: "a", content: []byte{}},
		"bad fingerprint":  {fields: map[string]string{"label": "x", "fingerprint": "nothex"}, filename: "a", content: []byte("x")},
		"control in label": {fields: map[string]string{"label": "a\x00b"}, filename: "a", content: []byte("x")},
		"too large":        {fields: map[string]string{"label": "x"}, filename: "a", content: make([]byte, 5<<20+10)},
	}
	for name, f := range cases {
		code, _ := upload(t, h, ctx, f)
		assert.Equal(t, http.StatusBadRequest, code, name)
	}
	code, _ := upload(t, h, context.Background(), form{fields: map[string]string{"label": "x"}, filename: "a", content: []byte("x")})
	assert.Equal(t, http.StatusUnauthorized, code, "no user and no host")
}

func TestVault_ReUploadingTheSameDocumentIsRecognised(t *testing.T) {
	h, store, _ := newVault()
	user := uuid.New()
	fp := strings.Repeat("ab", 32)
	f := form{fields: map[string]string{"label": "Resume", "category": "resume", "fingerprint": strings.ToUpper(fp)}, filename: "r.md", content: []byte("v1")}
	code, d1 := upload(t, h, asUser(user), f)
	require.Equal(t, http.StatusCreated, code)
	code, d2 := upload(t, h, asUser(user), f)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, d2["duplicate"])
	assert.Equal(t, d1["entry"].(map[string]any)["id"], d2["entry"].(map[string]any)["id"])
	assert.Equal(t, 1, store.saves, "a mirrored folder must not upload the same file twice")

	// another user's identical fingerprint is not a duplicate of this one
	code, d3 := upload(t, h, asUser(uuid.New()), f)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, false, d3["duplicate"])
}

func TestVault_ListIsPerOwnerNewestFirstAndNeverContainsFiles(t *testing.T) {
	h, store, _ := newVault()
	a, b := uuid.New(), uuid.New()
	for _, label := range []string{"First", "Second"} {
		upload(t, h, asUser(a), form{fields: map[string]string{"label": label}, filename: label + ".txt", content: []byte("secret body " + label)})
		time.Sleep(2 * time.Millisecond)
	}
	upload(t, h, asUser(b), form{fields: map[string]string{"label": "Theirs"}, filename: "t.txt", content: []byte("x")})
	// an ordinary artifact that is not a vault document must not appear
	store.arts = append(store.arts, &domain.Artifact{ID: uuid.New(), UserID: a, Metadata: json.RawMessage(`{"kind":"list"}`)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vault", nil).WithContext(asUser(a))
	rec := httptest.NewRecorder()
	h.List(rec, req)
	require.Equal(t, 200, rec.Code)
	var env struct{ Data []VaultEntry }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Data, 2)
	assert.Equal(t, "Second", env.Data[0].Label)
	assert.NotContains(t, rec.Body.String(), "secret body")
	assert.NotContains(t, rec.Body.String(), "Theirs")
}

func hostCtx(user uuid.UUID, kind domain.HostKind, granted, advertised []string) context.Context {
	return middleware.WithHost(context.Background(), &domain.Host{ID: uuid.New(), UserID: user, Kind: kind, Capabilities: granted, Advertised: advertised})
}

func TestVault_AHostNeedsTheFilesCapabilityAndActsForItsOwner(t *testing.T) {
	h, store, _ := newVault()
	owner := uuid.New()
	f := form{fields: map[string]string{"label": "Resume", "category": "resume"}, filename: "r.md", content: []byte("hi")}

	code, _ := upload(t, h, hostCtx(owner, domain.HostKindLaptop, []string{"agent"}, []string{"agent", "files"}), f)
	assert.Equal(t, http.StatusForbidden, code, "files not granted by the owner")
	code, _ = upload(t, h, hostCtx(owner, domain.HostKindLaptop, []string{"files"}, []string{"agent"}), f)
	assert.Equal(t, http.StatusForbidden, code, "files not offered by the host")

	code, data := upload(t, h, hostCtx(owner, domain.HostKindLaptop, []string{"files"}, []string{"files"}), f)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "laptop", data["entry"].(map[string]any)["source"])
	assert.Equal(t, owner, store.arts[0].UserID, "the document belongs to the host's owner")
}

func TestVault_MissesAreRecordedBumpedCappedAndResolved(t *testing.T) {
	h, _, store := newVault()
	user := uuid.New()
	post := func(body string) (int, domain.VaultMiss) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/vault/misses", strings.NewReader(body)).WithContext(asUser(user))
		rec := httptest.NewRecorder()
		h.AddMiss(rec, req)
		var env struct{ Data domain.VaultMiss }
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		return rec.Code, env.Data
	}
	list := func() []domain.VaultMiss {
		rec := httptest.NewRecorder()
		h.ListMisses(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(asUser(user)))
		var env struct{ Data []domain.VaultMiss }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		return env.Data
	}
	require.Empty(t, list())

	taskID := uuid.NewString()
	code, m1 := post(`{"query":"  PAN card   image ","task_id":"` + taskID + `"}`)
	require.Equal(t, 200, code)
	assert.Equal(t, "PAN card image", m1.Query)
	assert.Equal(t, 1, m1.Count)
	code, m2 := post(`{"query":"pan CARD image"}`)
	require.Equal(t, 200, code)
	assert.Equal(t, m1.ID, m2.ID, "the same need is one line")
	assert.Equal(t, 2, m2.Count)
	assert.Equal(t, taskID, m2.TaskID, "the task that asked is kept")
	require.Len(t, list(), 1)

	for _, bad := range []string{`{"query":""}`, `{"query":"` + strings.Repeat("a", 201) + `"}`, `{"query":"x\u0000y"}`, `{"query":"x","task_id":"nope"}`, `{`} {
		code, _ := post(bad)
		assert.Equal(t, http.StatusBadRequest, code, bad)
	}

	for i := 0; i < 70; i++ {
		post(`{"query":"document number ` + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + `"}`)
		time.Sleep(time.Millisecond)
	}
	assert.LessOrEqual(t, len(list()), 50, "the list is capped")

	// resolve one
	target := list()[0]
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vault/misses/"+target.ID, nil).WithContext(asUser(user))
	req.SetPathValue("id", target.ID)
	rec := httptest.NewRecorder()
	h.ResolveMiss(rec, req)
	require.Equal(t, 200, rec.Code)
	for _, m := range list() {
		assert.NotEqual(t, target.ID, m.ID)
	}
	req = httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(asUser(user))
	req.SetPathValue("id", "does-not-exist")
	rec = httptest.NewRecorder()
	h.ResolveMiss(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// damaged stored data does not block new misses
	store.raw = []byte("{not json")
	code, _ = post(`{"query":"after damage"}`)
	assert.Equal(t, 200, code)
	assert.Len(t, list(), 1)
	_ = errors.New
}
