package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
	"github.com/griffin/core/internal/vault"
)

// VaultStorage is the part of the storage service the Vault needs.
type VaultStorage interface {
	SaveArtifact(ctx context.Context, in storage.SaveArtifactInput) (*domain.Artifact, error)
	ListByKind(ctx context.Context, userID uuid.UUID, kind string) ([]*domain.Artifact, error)
}

// MissStore keeps the list of documents that were needed and were not there.
type MissStore interface {
	VaultMisses(ctx context.Context, userID uuid.UUID) ([]byte, error)
	SetVaultMisses(ctx context.Context, userID uuid.UUID, misses []byte) error
}

// VaultHandler serves the Vault: the owner's documents, kept in S3 as artifacts. Downloading and deleting
// use the existing artifact endpoints (presigned links, ownership checks); this handler adds what is
// specific to the Vault: listing, the rule that sensitive documents arrive already encrypted, de-duplication
// and the record of documents that were needed and missing.
type VaultHandler struct {
	store     VaultStorage
	misses    MissStore
	maxUpload int64
	now       func() time.Time
}

// NewVaultHandler constructs a VaultHandler. maxUpload bounds one document (default 5 MiB, the Lambda limit).
func NewVaultHandler(store VaultStorage, misses MissStore, maxUpload int64) *VaultHandler {
	if maxUpload <= 0 || maxUpload > 5<<20 {
		maxUpload = 5 << 20
	}
	return &VaultHandler{store: store, misses: misses, maxUpload: maxUpload, now: time.Now}
}

// VaultEntry is what the apps see of a document. It never includes the file.
type VaultEntry struct {
	ID          string               `json:"id"`
	Label       string               `json:"label"`
	Category    domain.VaultCategory `json:"category"`
	Sensitive   bool                 `json:"sensitive"`
	Encrypted   bool                 `json:"encrypted"`
	Filename    string               `json:"filename,omitempty"`
	SizeBytes   int64                `json:"size_bytes"`
	Source      string               `json:"source,omitempty"`
	Fingerprint string               `json:"fingerprint,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
}

func entryOf(a *domain.Artifact, m domain.VaultMeta) VaultEntry {
	e := VaultEntry{ID: a.ID.String(), Label: m.Label, Category: m.Category, Sensitive: m.Sensitive, Encrypted: m.Encrypted,
		Source: m.Source, Fingerprint: m.Fingerprint, CreatedAt: a.CreatedAt}
	if a.Filename != nil {
		e.Filename = *a.Filename
	}
	if a.FileSizeBytes != nil {
		e.SizeBytes = *a.FileSizeBytes
	}
	return e
}

// owner identifies who is asking: the signed-in owner, or one of the owner's hosts that may touch files.
func (h *VaultHandler) owner(r *http.Request) (uuid.UUID, string, error) {
	if id, err := middleware.GetUserID(r.Context()); err == nil {
		return id, "web", nil
	}
	if host := middleware.GetHost(r.Context()); host != nil {
		if !host.Covers([]string{domain.HostCapFiles}) {
			return uuid.Nil, "", fmt.Errorf("%w: this device has not been given access to files", domain.ErrForbidden)
		}
		return host.UserID, string(host.Kind), nil
	}
	return uuid.Nil, "", domain.ErrUnauthorized
}

// List handles GET /api/v1/vault.
func (h *VaultHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.owner(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	arts, err := h.store.ListByKind(r.Context(), userID, domain.VaultKind)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	out := make([]VaultEntry, 0, len(arts))
	for _, a := range arts {
		if m, ok := domain.ParseVaultMeta(a.Metadata); ok {
			out = append(out, entryOf(a, m))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	httputil.JSON(w, http.StatusOK, out)
}

var fingerprintRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Add handles POST /api/v1/vault (multipart: file, label, category, fingerprint).
func (h *VaultHandler) Add(w http.ResponseWriter, r *http.Request) {
	userID, source, err := h.owner(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload+(1<<20))
	if err := r.ParseMultipartForm(h.maxUpload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httputil.ErrorJSON(w, fmt.Errorf("%w: a document is at most %d MB", domain.ErrValidation, h.maxUpload>>20))
			return
		}
		httputil.ErrorJSON(w, fmt.Errorf("%w: expected a multipart form with a file", domain.ErrValidation))
		return
	}
	label, err := domain.NormalizeVaultLabel(r.FormValue("label"))
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	category := domain.VaultCategory(strings.ToLower(strings.TrimSpace(r.FormValue("category"))))
	if category == "" {
		category = domain.VaultOther
	}
	if !category.Valid() {
		httputil.ErrorJSON(w, fmt.Errorf("%w: category must be id, finance, resume, certificate or other", domain.ErrValidation))
		return
	}
	fingerprint := strings.ToLower(strings.TrimSpace(r.FormValue("fingerprint")))
	if fingerprint != "" && !fingerprintRe.MatchString(fingerprint) {
		httputil.ErrorJSON(w, fmt.Errorf("%w: fingerprint must be a SHA-256 in hex", domain.ErrValidation))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: the form field 'file' is required", domain.ErrValidation))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, h.maxUpload+1))
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if int64(len(data)) > h.maxUpload {
		httputil.ErrorJSON(w, fmt.Errorf("%w: a document is at most %d MB", domain.ErrValidation, h.maxUpload>>20))
		return
	}
	if len(data) == 0 {
		httputil.ErrorJSON(w, fmt.Errorf("%w: the file is empty", domain.ErrValidation))
		return
	}

	encrypted := vault.IsEncrypted(data)
	// The rule that matters: identity and finance documents are encrypted on the device before upload. The
	// server cannot tell a passport scan from a cat photo, so it cannot ask for proof beyond the format; but it
	// can refuse anything plainly unencrypted in a sensitive category, so a client bug or a hurried upload
	// cannot put one in the cloud in the clear.
	sensitive := category.Sensitive()
	if sensitive && !encrypted {
		httputil.ErrorJSON(w, fmt.Errorf("%w: %s documents must be encrypted on your device before they are uploaded", domain.ErrValidation, category))
		return
	}

	if fingerprint != "" {
		if existing, err := h.store.ListByKind(r.Context(), userID, domain.VaultKind); err == nil {
			for _, a := range existing {
				if m, ok := domain.ParseVaultMeta(a.Metadata); ok && m.Fingerprint == fingerprint {
					httputil.JSON(w, http.StatusOK, map[string]any{"duplicate": true, "entry": entryOf(a, m)})
					return
				}
			}
		}
	}

	meta, _ := json.Marshal(domain.VaultMeta{Kind: domain.VaultKind, Label: label, Category: category, Sensitive: sensitive,
		Encrypted: encrypted, Source: source, Fingerprint: fingerprint})
	contentType := "application/octet-stream"
	if !encrypted {
		if ct := header.Header.Get("Content-Type"); ct != "" {
			contentType = ct
		}
	}
	created, err := h.store.SaveArtifact(r.Context(), storage.SaveArtifactInput{
		UserID: userID, Title: label, Filename: safeFilename(header.Filename, encrypted), ArtifactType: "FILE",
		ContentType: contentType, Body: bytes.NewReader(data), Metadata: meta,
	})
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	m, _ := domain.ParseVaultMeta(created.Metadata)
	if m.Kind == "" { // a store that does not echo metadata back
		m = domain.VaultMeta{Kind: domain.VaultKind, Label: label, Category: category, Sensitive: sensitive, Encrypted: encrypted, Source: source, Fingerprint: fingerprint}
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"duplicate": false, "entry": entryOf(created, m)})
}

// safeFilename keeps a readable base name and drops anything that could mislead (paths, control characters).
// An encrypted file gets a .gvlt suffix so nobody opens ciphertext by mistake.
func safeFilename(name string, encrypted bool) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		name = "document"
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	if encrypted && !strings.HasSuffix(name, ".gvlt") {
		name += ".gvlt"
	}
	return name
}

// ---- documents that were needed and were not there ----

const maxMisses = 50

func (h *VaultHandler) loadMisses(ctx context.Context, userID uuid.UUID) ([]domain.VaultMiss, error) {
	raw, err := h.misses.VaultMisses(ctx, userID)
	if err != nil {
		return nil, err
	}
	var list []domain.VaultMiss
	if len(raw) > 0 && json.Unmarshal(raw, &list) != nil {
		return nil, nil // damaged data is dropped rather than blocking new misses
	}
	return list, nil
}

// ListMisses handles GET /api/v1/vault/misses.
func (h *VaultHandler) ListMisses(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.owner(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	list, err := h.loadMisses(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if list == nil {
		list = []domain.VaultMiss{}
	}
	httputil.JSON(w, http.StatusOK, list)
}

// AddMiss handles POST /api/v1/vault/misses {"query": "PAN card image", "task_id": "..."}. Asking again
// for the same thing bumps its count instead of adding a second line.
func (h *VaultHandler) AddMiss(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.owner(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var body struct {
		Query  string `json:"query"`
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	query := strings.Join(strings.Fields(body.Query), " ")
	if query == "" || len([]rune(query)) > 200 || strings.ContainsFunc(query, unicode.IsControl) {
		httputil.ErrorJSON(w, fmt.Errorf("%w: say what document was needed, in under 200 characters", domain.ErrValidation))
		return
	}
	if body.TaskID != "" {
		if _, err := uuid.Parse(body.TaskID); err != nil {
			httputil.ErrorJSON(w, fmt.Errorf("%w: task_id must be a UUID", domain.ErrValidation))
			return
		}
	}
	list, err := h.loadMisses(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	at := h.now().UTC().Format(time.RFC3339)
	var hit *domain.VaultMiss
	for i := range list {
		if strings.EqualFold(list[i].Query, query) {
			list[i].Count++
			list[i].At = at
			if body.TaskID != "" {
				list[i].TaskID = body.TaskID
			}
			hit = &list[i]
			break
		}
	}
	if hit == nil {
		list = append(list, domain.VaultMiss{ID: uuid.NewString(), Query: query, TaskID: body.TaskID, Count: 1, At: at})
		hit = &list[len(list)-1]
		if len(list) > maxMisses { // keep the newest
			sort.SliceStable(list, func(i, j int) bool { return list[i].At > list[j].At })
			list = list[:maxMisses]
			found := false
			for i := range list {
				if list[i].ID == hit.ID {
					hit, found = &list[i], true
				}
			}
			if !found {
				hit = &list[0]
			}
		}
	}
	raw, _ := json.Marshal(list)
	if err := h.misses.SetVaultMisses(r.Context(), userID, raw); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, hit)
}

// ResolveMiss handles DELETE /api/v1/vault/misses/{id}: the document was added, or is no longer wanted.
func (h *VaultHandler) ResolveMiss(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.owner(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id := r.PathValue("id")
	list, err := h.loadMisses(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	kept := list[:0:0]
	found := false
	for _, m := range list {
		if m.ID == id {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	if !found {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}
	raw, _ := json.Marshal(kept)
	if err := h.misses.SetVaultMisses(r.Context(), userID, raw); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"resolved": id})
}
