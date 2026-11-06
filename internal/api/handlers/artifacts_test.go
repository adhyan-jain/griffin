package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
)

const testBucket = "griffin-artifacts"

type memArtifactRepo struct {
	mu   sync.Mutex
	arts map[uuid.UUID]*domain.Artifact
}

func (m *memArtifactRepo) CreateArtifact(_ context.Context, a *domain.Artifact) (*domain.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.CreatedAt = time.Now().UTC()
	m.arts[a.ID] = a
	return a, nil
}
func (m *memArtifactRepo) GetArtifactByID(_ context.Context, id uuid.UUID) (*domain.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.arts[id]; ok {
		return a, nil
	}
	return nil, domain.ErrNotFound
}
func (m *memArtifactRepo) GetArtifactByIDAndUser(_ context.Context, id, uid uuid.UUID) (*domain.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.arts[id]; ok && a.UserID == uid {
		return a, nil
	}
	return nil, domain.ErrNotFound
}
func (m *memArtifactRepo) GetArtifactByObjectKey(context.Context, string, string) (*domain.Artifact, error) {
	return nil, domain.ErrNotFound
}
func (m *memArtifactRepo) ListArtifacts(_ context.Context, uid uuid.UUID, taskID *uuid.UUID) ([]*domain.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*domain.Artifact
	for _, a := range m.arts {
		if a.UserID == uid && (taskID == nil || (a.TaskID != nil && *a.TaskID == *taskID)) {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memArtifactRepo) DeleteArtifact(_ context.Context, id, uid uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.arts[id]; ok && a.UserID == uid {
		delete(m.arts, id)
		return nil
	}
	return domain.ErrNotFound
}

// memTaskRepo embeds the interface; only GetTaskByIDAndUser is exercised.
type memTaskRepo struct {
	repository.TaskRepository
	owner map[uuid.UUID]uuid.UUID
}

func (m *memTaskRepo) GetTaskByIDAndUser(_ context.Context, id, uid uuid.UUID) (*domain.Task, error) {
	if o, ok := m.owner[id]; ok && o == uid {
		return &domain.Task{ID: id, UserID: uid}, nil
	}
	return nil, domain.ErrNotFound
}

type env struct {
	h        *handlers.ArtifactHandler
	provider *storage.MockStorageProvider
	repo     *memArtifactRepo
	tasks    *memTaskRepo
}

func newEnv(maxUpload int64) *env {
	p := storage.NewMockStorageProvider()
	r := &memArtifactRepo{arts: map[uuid.UUID]*domain.Artifact{}}
	tr := &memTaskRepo{owner: map[uuid.UUID]uuid.UUID{}}
	svc := storage.NewStorageService(p, r, testBucket, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &env{h: handlers.NewArtifactHandler(r, svc, tr, maxUpload).WithPresignExpiry(20 * time.Minute), provider: p, repo: r, tasks: tr}
}

func (e *env) do(user uuid.UUID, fn http.HandlerFunc, req *http.Request, id string) *httptest.ResponseRecorder {
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, user))
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func get() *http.Request { return httptest.NewRequest(http.MethodGet, "/", nil) }

func multipartReq(t *testing.T, fields map[string]string, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if filename != "" {
		fw, _ := mw.CreateFormFile("file", filename)
		_, _ = fw.Write(content)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func decodeArtifact(t *testing.T, rec *httptest.ResponseRecorder) *domain.Artifact {
	t.Helper()
	var out struct {
		Data domain.Artifact `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return &out.Data
}

func upload(t *testing.T, e *env, user uuid.UUID, name string, content []byte) *domain.Artifact {
	t.Helper()
	rec := e.do(user, e.h.UploadArtifact, multipartReq(t, map[string]string{"title": "T"}, name, content), "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %q: %d %s", name, rec.Code, rec.Body.String())
	}
	return decodeArtifact(t, rec)
}

func TestArtifacts_UploadDownloadRoundTrip(t *testing.T) {
	e := newEnv(0)
	u := uuid.New()
	content := []byte("hello griffin")
	art := upload(t, e, u, "note.txt", content)
	if art.FileSizeBytes == nil || *art.FileSizeBytes != int64(len(content)) || art.Checksum == nil {
		t.Fatalf("metadata not persisted: %+v", art)
	}
	if _, err := e.repo.GetArtifactByIDAndUser(context.Background(), art.ID, u); err != nil {
		t.Fatalf("metadata row missing: %v", err)
	}
	rec := e.do(u, e.h.DownloadArtifact, get(), art.ID.String())
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), content) {
		t.Fatalf("download mismatch: %d %q", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "note.txt") {
		t.Fatalf("content-disposition: %q", cd)
	}
}

func TestArtifacts_RawUploadRoundTrip(t *testing.T) {
	e := newEnv(0)
	u := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/upload", strings.NewReader("raw body"))
	req.Header.Set("X-Title", "Raw")
	req.Header.Set("X-Filename", "raw.bin")
	req.Header.Set("X-Artifact-Type", "FILE")
	rec := e.do(u, e.h.UploadArtifact, req, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("raw upload: %d %s", rec.Code, rec.Body.String())
	}
	art := decodeArtifact(t, rec)
	if art.Title != "Raw" {
		t.Fatalf("title: %q", art.Title)
	}
	rec = e.do(u, e.h.DownloadArtifact, get(), art.ID.String())
	if rec.Body.String() != "raw body" {
		t.Fatalf("raw round trip mismatch: %q", rec.Body.String())
	}
}

func TestArtifacts_OwnershipIsolation(t *testing.T) {
	e := newEnv(0)
	a, b := uuid.New(), uuid.New()
	secret := []byte("user A secret")
	art := upload(t, e, a, "secret.txt", secret)
	id := art.ID.String()

	checks := map[string]*httptest.ResponseRecorder{
		"get":      e.do(b, e.h.GetArtifact, get(), id),
		"download": e.do(b, e.h.DownloadArtifact, get(), id),
		"presign":  e.do(b, e.h.PresignArtifact, get(), id),
		"delete":   e.do(b, e.h.DeleteArtifact, httptest.NewRequest(http.MethodDelete, "/", nil), id),
	}
	for name, rec := range checks {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "users/") {
			t.Errorf("%s leaked data: %s", name, rec.Body.String())
		}
	}
	list := e.do(b, e.h.ListArtifacts, get(), "")
	if strings.Contains(list.Body.String(), id) {
		t.Fatalf("list leaked foreign artifact")
	}
	rec := e.do(a, e.h.DownloadArtifact, get(), id)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), secret) {
		t.Fatalf("owner lost data after foreign delete attempt: %d", rec.Code)
	}
}

func TestArtifacts_ForeignAndInvalidTaskID(t *testing.T) {
	e := newEnv(0)
	a, b := uuid.New(), uuid.New()
	task := uuid.New()
	e.tasks.owner[task] = b

	rec := e.do(a, e.h.UploadArtifact, multipartReq(t, map[string]string{"task_id": task.String()}, "x.txt", []byte("x")), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign task upload: expected 404, got %d", rec.Code)
	}
	rec = e.do(a, e.h.UploadArtifact, multipartReq(t, map[string]string{"task_id": "not-a-uuid"}, "x.txt", []byte("x")), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid task_id multipart: expected 400, got %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	req.Header.Set("X-Task-ID", "nope")
	if rec = e.do(a, e.h.UploadArtifact, req, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid task_id raw: expected 400, got %d", rec.Code)
	}
	if rec = e.do(a, e.h.ListArtifacts, httptest.NewRequest(http.MethodGet, "/?task_id=zzz", nil), ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid task_id list: expected 400, got %d", rec.Code)
	}
	if len(e.repo.arts) != 0 {
		t.Fatalf("rejected uploads must not persist metadata")
	}
}

func TestArtifacts_InvalidArtifactID(t *testing.T) {
	e := newEnv(0)
	u := uuid.New()
	for name, fn := range map[string]http.HandlerFunc{
		"get": e.h.GetArtifact, "download": e.h.DownloadArtifact, "presign": e.h.PresignArtifact, "delete": e.h.DeleteArtifact,
	} {
		if rec := e.do(u, fn, get(), "not-a-uuid"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, rec.Code)
		}
	}
}

func TestArtifacts_PresignExpiry(t *testing.T) {
	e := newEnv(0)
	u := uuid.New()
	art := upload(t, e, u, "p.txt", []byte("p"))

	var out struct {
		Data struct {
			URL       string    `json:"url"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"data"`
	}
	rec := e.do(u, e.h.PresignArtifact, get(), art.ID.String())
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || !strings.Contains(out.Data.URL, "X-Amz-Expires=1200") {
		t.Fatalf("default should use configured 20m: %d %s", rec.Code, out.Data.URL)
	}
	if d := time.Until(out.Data.ExpiresAt); d < 19*time.Minute || d > 20*time.Minute+5*time.Second {
		t.Fatalf("expires_at not ~20m away: %v", d)
	}
	rec = e.do(u, e.h.PresignArtifact, httptest.NewRequest(http.MethodGet, "/?expires_in=1m", nil), art.ID.String())
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(out.Data.URL, "X-Amz-Expires=60") {
		t.Fatalf("shorten failed: %s", out.Data.URL)
	}
	rec = e.do(u, e.h.PresignArtifact, httptest.NewRequest(http.MethodGet, "/?expires_in=720h", nil), art.ID.String())
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(out.Data.URL, "X-Amz-Expires=1200") {
		t.Fatalf("expires_in must be capped to configured TTL: %s", out.Data.URL)
	}
}

func TestArtifacts_OversizeRejected413(t *testing.T) {
	e := newEnv(1024)
	u := uuid.New()
	big := bytes.Repeat([]byte("a"), 4096)

	rec := e.do(u, e.h.UploadArtifact, multipartReq(t, nil, "big.bin", big), "")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("multipart: expected 413, got %d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(big))
	rec = e.do(u, e.h.UploadArtifact, req, "")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("raw: expected 413, got %d %s", rec.Code, rec.Body.String())
	}

	// No Content-Length (chunked): body of unknown length.
	req = httptest.NewRequest(http.MethodPost, "/", io.MultiReader(bytes.NewReader(big)))
	req.ContentLength = -1
	rec = e.do(u, e.h.UploadArtifact, req, "")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("raw no content-length: expected 413, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "1024") {
		t.Fatalf("message should state the limit: %s", rec.Body.String())
	}
	if len(e.repo.arts) != 0 {
		t.Fatalf("oversize must not persist metadata")
	}
}

func TestArtifacts_MissingFileField(t *testing.T) {
	e := newEnv(0)
	rec := e.do(uuid.New(), e.h.UploadArtifact, multipartReq(t, map[string]string{"title": "x"}, "", nil), "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "file") {
		t.Fatalf("expected 400 naming 'file', got %d %s", rec.Code, rec.Body.String())
	}
}

func TestArtifacts_FilenameSanitisation(t *testing.T) {
	e := newEnv(0)
	u := uuid.New()
	for _, name := range []string{`../../etc/passwd`, `..\..\win.ini`, `..`, "a\"b\r\nX-Evil: 1.txt", "a b.txt"} {
		art := upload(t, e, u, name, []byte("x"))
		key := *art.ObjectKey
		if !strings.HasPrefix(key, storage.UserKeyPrefix(u)) {
			t.Errorf("%q: key escaped user prefix: %s", name, key)
		}
		for _, seg := range strings.Split(key, "/") {
			if seg == ".." || seg == "." || strings.ContainsAny(seg, "\\\"\r\n") {
				t.Errorf("%q: unsafe key segment in %q", name, key)
			}
		}
		rec := e.do(u, e.h.DownloadArtifact, get(), art.ID.String())
		if rec.Code != http.StatusOK {
			t.Errorf("%q: download after upload failed: %d", name, rec.Code)
		}
		if strings.ContainsAny(rec.Header().Get("Content-Disposition"), "\r\n") {
			t.Errorf("%q: header injection", name)
		}
	}
}

func TestArtifacts_SameFilenameSameTaskDoesNotCollide(t *testing.T) {
	e := newEnv(0)
	u, task := uuid.New(), uuid.New()
	e.tasks.owner[task] = u
	up := func(c string) *domain.Artifact {
		rec := e.do(u, e.h.UploadArtifact, multipartReq(t, map[string]string{"task_id": task.String()}, "out.txt", []byte(c)), "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
		}
		return decodeArtifact(t, rec)
	}
	first, second := up("one"), up("two")
	if *first.ObjectKey == *second.ObjectKey {
		t.Fatalf("two artifacts share one object key")
	}
	e.do(u, e.h.DeleteArtifact, httptest.NewRequest(http.MethodDelete, "/", nil), first.ID.String())
	rec := e.do(u, e.h.DownloadArtifact, get(), second.ID.String())
	if rec.Body.String() != "two" {
		t.Fatalf("deleting one artifact destroyed the other's object")
	}
}

func TestArtifacts_DeleteRemovesMetadataAndObject(t *testing.T) {
	e := newEnv(0)
	u := uuid.New()
	art := upload(t, e, u, "d.txt", []byte("d"))
	rec := e.do(u, e.h.DeleteArtifact, httptest.NewRequest(http.MethodDelete, "/", nil), art.ID.String())
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, err := e.repo.GetArtifactByID(context.Background(), art.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("metadata still present")
	}
	if _, err := e.provider.Head(context.Background(), storage.HeadInput{Bucket: testBucket, Key: *art.ObjectKey}); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("object still present: %v", err)
	}
}

func TestArtifacts_DeleteObjectFailureKeepsMetadataAndIsNonLeaky(t *testing.T) {
	e := newEnv(0)
	u := uuid.New()
	art := upload(t, e, u, "d.txt", []byte("d"))
	e.provider.FailDelete = errors.New("AccessDenied: arn:aws:iam::123456789012:user/x is not authorized")
	rec := e.do(u, e.h.DeleteArtifact, httptest.NewRequest(http.MethodDelete, "/", nil), art.ID.String())
	if rec.Code < 500 {
		t.Fatalf("expected 5xx when object delete fails, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "123456789012") {
		t.Fatalf("leaked AWS detail: %s", rec.Body.String())
	}
	if _, err := e.repo.GetArtifactByID(context.Background(), art.ID); err != nil {
		t.Fatalf("metadata must be retained so delete can be retried: %v", err)
	}
	e.provider.FailDelete = nil
	rec = e.do(u, e.h.DeleteArtifact, httptest.NewRequest(http.MethodDelete, "/", nil), art.ID.String())
	if rec.Code != http.StatusNoContent {
		t.Fatalf("retry delete: %d", rec.Code)
	}
}

func TestArtifacts_StorageErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		bad  string
	}{
		{"bucket missing", storage.ErrBucketNotFound, http.StatusServiceUnavailable, "bucket not found"},
		{"access denied", storage.ErrPermissionDenied, http.StatusBadGateway, "denied"},
		{"raw aws error", errors.New("operation error S3: PutObject, arn:aws:s3:::secret-bucket"), http.StatusInternalServerError, "secret-bucket"},
	}
	for _, c := range cases {
		e := newEnv(0)
		e.provider.FailUpload = c.err
		rec := e.do(uuid.New(), e.h.UploadArtifact, multipartReq(t, nil, "x.txt", []byte("x")), "")
		if rec.Code != c.code {
			t.Errorf("%s: expected %d got %d (%s)", c.name, c.code, rec.Code, rec.Body.String())
		}
		if strings.Contains(strings.ToLower(rec.Body.String()), c.bad) {
			t.Errorf("%s: response leaks %q: %s", c.name, c.bad, rec.Body.String())
		}
		if len(e.repo.arts) != 0 {
			t.Errorf("%s: metadata persisted despite failed upload", c.name)
		}
	}
}

func (m *memArtifactRepo) UpdateArtifactContentInfo(_ context.Context, id uuid.UUID, userID uuid.UUID, size int64, checksum string) (*domain.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.arts[id]
	if !ok || a.UserID != userID {
		return nil, domain.ErrNotFound
	}
	a.FileSizeBytes = &size
	a.Checksum = &checksum
	return a, nil
}

func jsonReq(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// A living list can be created, grown by appending, replaced, and only its owner can touch it.
func TestLists_CreateAppendReplaceAndOwnership(t *testing.T) {
	e := newEnv(1 << 20)
	owner, other := uuid.New(), uuid.New()

	rec := e.do(owner, e.h.CreateList, jsonReq(http.MethodPost, `{"title":"Internship tracker","content":"# Internship tracker\n| Company | Status |\n|---|---|"}`), "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct{ Data domain.Artifact }
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	id := ""
	if d, ok := raw["data"].(map[string]any); ok {
		id, _ = d["id"].(string)
		_ = created
		if !strings.Contains(string(rec.Body.Bytes()), `"kind":"list"`) && !strings.Contains(rec.Body.String(), `\"kind\":\"list\"`) && !strings.Contains(rec.Body.String(), `"kind": "list"`) {
			t.Fatalf("list is not marked as a list: %s", rec.Body.String())
		}
	} else if v, ok := raw["id"].(string); ok {
		id = v
	}
	if id == "" {
		t.Fatalf("no id in response: %s", rec.Body.String())
	}

	body := func(user uuid.UUID) string {
		d := e.do(user, e.h.DownloadArtifact, get(), id)
		return d.Body.String()
	}

	if rec := e.do(owner, e.h.UpdateArtifactText, jsonReq(http.MethodPut, `{"text":"| Acme | Applied |","mode":"append"}`), id); rec.Code != http.StatusOK {
		t.Fatalf("append: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(owner, e.h.UpdateArtifactText, jsonReq(http.MethodPut, `{"text":"| Globex | Interview |","mode":"append"}`), id); rec.Code != http.StatusOK {
		t.Fatalf("second append: %d", rec.Code)
	}
	got := body(owner)
	if !strings.Contains(got, "| Acme | Applied |") || !strings.Contains(got, "| Globex | Interview |") || !strings.HasPrefix(got, "# Internship tracker") {
		t.Fatalf("appends must keep earlier content and add new rows:\n%s", got)
	}

	if rec := e.do(owner, e.h.UpdateArtifactText, jsonReq(http.MethodPut, `{"text":"# Fresh start"}`), id); rec.Code != http.StatusOK {
		t.Fatalf("replace: %d", rec.Code)
	}
	if got := body(owner); strings.TrimSpace(got) != "# Fresh start" {
		t.Fatalf("replace must overwrite: %q", got)
	}

	// Another user can neither read nor change it.
	if rec := e.do(other, e.h.UpdateArtifactText, jsonReq(http.MethodPut, `{"text":"hijack","mode":"append"}`), id); rec.Code != http.StatusNotFound {
		t.Fatalf("other user must get 404, got %d", rec.Code)
	}
	if got := body(owner); strings.Contains(got, "hijack") {
		t.Fatal("another user's edit leaked into the list")
	}

	// Validation: bad mode, empty append, missing title, oversize.
	for name, req := range map[string]*http.Request{
		"bad mode":     jsonReq(http.MethodPut, `{"text":"x","mode":"merge"}`),
		"empty append": jsonReq(http.MethodPut, `{"text":"  ","mode":"append"}`),
	} {
		if rec := e.do(owner, e.h.UpdateArtifactText, req, id); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, rec.Code)
		}
	}
	if rec := e.do(owner, e.h.CreateList, jsonReq(http.MethodPost, `{"title":"  "}`), ""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing title: want 400, got %d", rec.Code)
	}
	small := newEnv(64)
	rec = small.do(owner, small.h.CreateList, jsonReq(http.MethodPost, `{"title":"Tiny","content":"ok"}`), "")
	var rr map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rr)
	sid, _ := rr["id"].(string)
	if d, ok := rr["data"].(map[string]any); ok {
		sid, _ = d["id"].(string)
	}
	if rec := small.do(owner, small.h.UpdateArtifactText, jsonReq(http.MethodPut, `{"text":"`+strings.Repeat("a", 200)+`","mode":"append"}`), sid); rec.Code == http.StatusOK {
		t.Error("an append past the size limit must be rejected")
	}
}
