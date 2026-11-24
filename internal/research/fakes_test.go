package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

// ---- fake model ----

type recordedCall struct{ kind, system, user string }

// fakeLLM answers by the kind of task its system prompt names, from canned replies, and records every call.
type fakeLLM struct {
	mu      sync.Mutex
	calls   []recordedCall
	plan    func() (string, error)
	extract map[string]string // page URL (found in the prompt) -> reply
	gap     func(user string) (string, error)
	synth   func() (string, error)
}

const defaultPlan = `{"questions":[{"id":"q1","question":"What does AcmeProcure do?","category":"overview"},{"id":"q2","question":"How many engineers is AcmeProcure hiring?","category":"hiring"}],"initial_queries":["acmeprocure overview"]}`

func (f *fakeLLM) Chat(_ context.Context, system string, turns []gemini.Turn) (string, error) {
	user := ""
	for _, t := range turns {
		user += t.Text
	}
	kind := "synthesis"
	switch {
	case strings.Contains(system, "principal research investigator"):
		kind = "plan"
	case strings.Contains(system, "evidence extraction engine"):
		kind = "extract"
	case strings.Contains(system, "research gap auditor"):
		kind = "gap"
	}
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{kind, system, user})
	f.mu.Unlock()

	switch kind {
	case "plan":
		if f.plan != nil {
			return f.plan()
		}
		return defaultPlan, nil
	case "extract":
		f.mu.Lock()
		defer f.mu.Unlock()
		// the URL is the first thing in the fenced block
		for u, reply := range f.extract {
			if strings.Contains(user, "URL: "+u) {
				return reply, nil
			}
		}
		return `{"findings":[],"new_leads":[]}`, nil
	case "gap":
		if f.gap != nil {
			return f.gap(user)
		}
		return `{"answers":[]}`, nil
	}
	if f.synth != nil {
		return f.synth()
	}
	return "Synthesized analysis of the findings.", nil
}

func (f *fakeLLM) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.kind == kind {
			n++
		}
	}
	return n
}

func (f *fakeLLM) last(kind string) recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].kind == kind {
			return f.calls[i]
		}
	}
	return recordedCall{}
}

// ---- fake search and fetch ----

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeTools returns fixed search results and pages, and records every call.
type fakeTools struct {
	mu       sync.Mutex
	results  map[string][]SearchHit
	pages    map[string]string
	searches []string
	fetches  []string
	onSearch func(q string)
	onFetch  func(u string)
	clock    *fakeClock
	cost     time.Duration // fake time each call takes
}

func newFakeTools() *fakeTools {
	return &fakeTools{results: map[string][]SearchHit{}, pages: map[string]string{}}
}

func (f *fakeTools) search(query string, urls ...string) {
	var hits []SearchHit
	for _, u := range urls {
		hits = append(hits, SearchHit{Title: "Hit " + u, URL: u, Domain: extractDomain(u), Snippet: "snippet"})
	}
	f.results[strings.ToLower(query)] = hits
}

func (f *fakeTools) Search(_ context.Context, query string, _ int) ([]SearchHit, error) {
	f.mu.Lock()
	f.searches = append(f.searches, query)
	hook, hits := f.onSearch, f.results[strings.ToLower(query)]
	f.mu.Unlock()
	if f.clock != nil {
		f.clock.advance(f.cost)
	}
	if hook != nil {
		hook(query)
	}
	return hits, nil
}

func (f *fakeTools) Fetch(_ context.Context, rawURL string) (*FetchedContent, error) {
	f.mu.Lock()
	f.fetches = append(f.fetches, rawURL)
	hook := f.onFetch
	text, ok := f.pages[rawURL]
	f.mu.Unlock()
	if f.clock != nil {
		f.clock.advance(f.cost)
	}
	if hook != nil {
		hook(rawURL)
	}
	if !ok {
		return nil, errors.New("404 not found")
	}
	return &FetchedContent{URL: rawURL, Title: "Page " + rawURL, Domain: extractDomain(rawURL), Text: text,
		Status: 200, ContentHash: domain.DescriptionHash(text)}, nil
}

func (f *fakeTools) searchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.searches)
}

func (f *fakeTools) fetchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fetches)
}

// ---- in-memory store with the database's guarantees ----

type stepKey struct {
	run uuid.UUID
	idx int
}

type memResearchRepo struct {
	mu       sync.Mutex
	runs     map[uuid.UUID]*domain.ResearchRun
	steps    map[stepKey]*domain.ResearchStep
	sources  map[string]*domain.ResearchSource // run + canonical url
	evidence []*domain.EvidenceItem

	updates      int // UpdateRun calls so far
	failUpdateAt int // when > 0, that UpdateRun call fails as if the process died before saving
}

func newMemResearchRepo() *memResearchRepo {
	return &memResearchRepo{
		runs:    map[uuid.UUID]*domain.ResearchRun{},
		steps:   map[stepKey]*domain.ResearchStep{},
		sources: map[string]*domain.ResearchSource{},
	}
}

func cloneRun(r *domain.ResearchRun) *domain.ResearchRun {
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	var out domain.ResearchRun
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return &out
}

func (m *memResearchRepo) CreateRun(_ context.Context, r *domain.ResearchRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ID = uuid.New()
	r.CreatedAt, r.UpdatedAt = time.Now(), time.Now()
	r.Revision = 0
	m.runs[r.ID] = cloneRun(r)
	return nil
}

func (m *memResearchRepo) GetRun(_ context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[runID]
	if r == nil || r.UserID != userID {
		return nil, domain.ErrNotFound
	}
	return cloneRun(r), nil
}

func (m *memResearchRepo) UpdateRun(_ context.Context, r *domain.ResearchRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates++
	if m.failUpdateAt > 0 && m.updates == m.failUpdateAt {
		return errors.New("simulated crash before the run was saved")
	}
	stored := m.runs[r.ID]
	if stored == nil || stored.UserID != r.UserID {
		return domain.ErrNotFound
	}
	if stored.Revision != r.Revision || !isActive(stored.Status) {
		return domain.ErrResearchRunInactive
	}
	r.Revision++
	m.runs[r.ID] = cloneRun(r)
	return nil
}

func (m *memResearchRepo) TransitionRun(_ context.Context, userID, runID uuid.UUID, to domain.ResearchPhase,
	from []domain.ResearchPhase, stopReason string, errMsg *string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[runID]
	if r == nil || r.UserID != userID {
		return false, nil
	}
	allowed := false
	for _, f := range from {
		allowed = allowed || f == r.Status
	}
	if !allowed {
		return false, nil
	}
	r.Status = to
	if stopReason != "" {
		r.StopReason = stopReason
	}
	if errMsg != nil {
		r.ErrorMessage = errMsg
	}
	r.Revision++
	if to.IsTerminal() {
		now := time.Now()
		r.CompletedAt = &now
	}
	return true, nil
}

func (m *memResearchRepo) ListRuns(_ context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*domain.ResearchRun
	for _, r := range m.runs {
		if r.UserID == userID && len(out) < limit {
			out = append(out, cloneRun(r))
		}
	}
	return out, nil
}

func (m *memResearchRepo) RecordStep(_ context.Context, s *domain.ResearchStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := stepKey{s.RunID, s.StepIndex}
	if old := m.steps[k]; old != nil {
		if old.Status == "done" || old.Status == "completed" {
			return domain.ErrResearchStepDone
		}
		cp := *s
		cp.ID = old.ID
		m.steps[k] = &cp
		s.ID = old.ID
		return nil
	}
	s.ID = uuid.New()
	cp := *s
	m.steps[k] = &cp
	return nil
}

func (m *memResearchRepo) ListSteps(_ context.Context, userID, runID uuid.UUID) ([]*domain.ResearchStep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*domain.ResearchStep
	for k, s := range m.steps {
		if k.run == runID && s.UserID == userID {
			cp := *s
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StepIndex < out[j].StepIndex })
	return out, nil
}

func (m *memResearchRepo) AddSource(_ context.Context, s *domain.ResearchSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := s.RunID.String() + "|" + s.CanonicalURL
	if old := m.sources[k]; old != nil {
		old.ContentHash, old.Title, old.RetrievedAt = s.ContentHash, s.Title, s.RetrievedAt
		s.ID = old.ID
		return nil
	}
	s.ID = uuid.New()
	cp := *s
	m.sources[k] = &cp
	return nil
}

func (m *memResearchRepo) AddEvidence(_ context.Context, e *domain.EvidenceItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.Confidence < 0 || e.Confidence > 100 { // the database CHECK
		return fmt.Errorf("evidence_items_confidence_check: %d", e.Confidence)
	}
	switch e.Status {
	case domain.EvidenceSupported, domain.EvidenceUnverified, domain.EvidenceConflicting, domain.EvidenceDiscovered:
	default:
		return fmt.Errorf("evidence status %q violates the check", e.Status)
	}
	if e.DedupeKey != "" {
		for _, old := range m.evidence {
			if old.RunID != nil && e.RunID != nil && *old.RunID == *e.RunID && old.DedupeKey == e.DedupeKey {
				e.ID, e.Status = old.ID, old.Status
				return nil
			}
		}
	}
	e.ID = uuid.New()
	cp := *e
	m.evidence = append(m.evidence, &cp)
	return nil
}

func (m *memResearchRepo) SetEvidenceStatus(_ context.Context, userID, id uuid.UUID, st domain.EvidenceStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.evidence {
		if e.ID == id && e.UserID == userID {
			e.Status = st
			return nil
		}
	}
	return domain.ErrNotFound
}

func (m *memResearchRepo) ListEvidenceForRun(_ context.Context, userID, runID uuid.UUID) ([]*domain.EvidenceItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*domain.EvidenceItem
	for _, e := range m.evidence {
		if e.UserID == userID && e.RunID != nil && *e.RunID == runID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memResearchRepo) sourceCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sources)
}

func (m *memResearchRepo) evidenceList() []*domain.EvidenceItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*domain.EvidenceItem(nil), m.evidence...)
}

// ---- scenario ----

const (
	aboutURL  = "https://acme.example.com/about"
	newsURL   = "https://news.example.com/acme-hiring"
	aboutText = "AcmeProcure builds procurement software. AcmeProcure announced a $10M Series A in March 2026."
	newsText  = "Local news. AcmeProcure is hiring 40 engineers this year across Bengaluru and Pune."
)

func aboutReply(quote string) string {
	return fmt.Sprintf(`{"findings":[{"topic":"AcmeProcure","claim":"AcmeProcure raised a $10M Series A","extracted_value":"$10M","subject":"AcmeProcure","property":"series a","question_id":"q1","quote":%q,"confidence":90}]}`, quote)
}

const newsReply = `{"findings":[{"topic":"Hiring","claim":"AcmeProcure is hiring 40 engineers","extracted_value":"40","question_id":"q2","quote":"AcmeProcure is hiring 40 engineers this year","confidence":80}]}`

type scenario struct {
	repo  *memResearchRepo
	tools *fakeTools
	llm   *fakeLLM
	eng   *Engine
	clock *fakeClock
	user  uuid.UUID
}

func newScenario() *scenario {
	clock := &fakeClock{t: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)}
	s := &scenario{repo: newMemResearchRepo(), tools: newFakeTools(), llm: &fakeLLM{extract: map[string]string{}}, clock: clock, user: uuid.New()}
	s.eng = NewEngine(s.repo, nil, s.tools, NewRouter(s.llm, s.llm), nil)
	s.eng.Now = clock.Now
	s.tools.search("acmeprocure overview", aboutURL)
	s.tools.pages[aboutURL] = aboutText
	s.tools.pages[newsURL] = newsText
	return s
}

func (s *scenario) start(t *testing.T, req domain.ResearchRequest) *domain.ResearchRun {
	t.Helper()
	if req.Objective == "" {
		req.Objective = "Research AcmeProcure"
	}
	run, err := s.eng.StartRun(context.Background(), s.user, req)
	require.NoError(t, err)
	return run
}

// finish advances the run until it is no longer active.
func (s *scenario) finish(t *testing.T, id uuid.UUID) *domain.ResearchRun {
	t.Helper()
	for i := 0; i < 20; i++ {
		run, _, err := s.eng.RunSteps(context.Background(), s.user, id)
		require.NoError(t, err)
		if !isActive(run.Status) {
			return run
		}
	}
	t.Fatal("run did not finish")
	return nil
}

var findingIDRe = regexp.MustCompile(`"id":"(f_[0-9a-f]+)"`)
