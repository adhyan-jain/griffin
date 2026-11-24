package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Why a research run stopped. Only StopAnswered and StopExhausted mean the search ran its natural course; every
// budget_* reason means the run was cut short and its coverage is incomplete.
const (
	StopAnswered         = "answered"
	StopExhausted        = "exhausted"
	StopBudgetSearches   = "budget_searches"
	StopBudgetPages      = "budget_pages"
	StopBudgetModelCalls = "budget_model_calls"
	StopBudgetTime       = "budget_time"
	StopCancelled        = "cancelled"
	StopFailed           = "failed"
)

// ErrResearchRunInactive is returned when a write to a run was refused because the run is finished or paused, or
// because another writer changed it first. The caller re-reads the run and stops or carries on from what it finds.
var ErrResearchRunInactive = errors.New("research run is no longer active or was changed by another writer")

// ErrResearchStepDone is returned when a step was already finished by an earlier attempt.
var ErrResearchStepDone = errors.New("research step already done")

// ResearchRunTitlePrefix starts the title of the task that advances a research run. The routine that handles it
// runs in code, under the request deadline, and creates the next task while work remains.
const ResearchRunTitlePrefix = "RESEARCH RUN "

// ResearchRunIDOf reads the run a task advances from its title ("RESEARCH RUN <run id>").
func ResearchRunIDOf(title string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(title)), ResearchRunTitlePrefix)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(strings.TrimSpace(rest))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// NewResearchRunTask is the task that advances a run from the point it has reached (its next step index and
// revision). The idempotency key makes a duplicate request for the same point in the run a no-op, so a double
// claim cannot fork the run, while a resume after a pause (which changes the revision) still gets its own task.
func NewResearchRunTask(userID, runID uuid.UUID, nextStep int, revision int64, objective string) *Task {
	key := fmt.Sprintf("research:%s:%d:%d", runID, nextStep, revision)
	goal := strings.TrimSpace(objective)
	if r := []rune(goal); len(r) > 200 {
		goal = string(r[:200])
	}
	return &Task{
		ID:             uuid.New(),
		UserID:         userID,
		Title:          ResearchRunTitlePrefix + runID.String(),
		Goal:           "Advance research run " + runID.String() + ": " + goal,
		ManagerDomain:  "research",
		Priority:       TaskPriorityNormalP2,
		IdempotencyKey: &key,
		MaxRetries:     2,
	}
}

// ResearchAdvance says what one invocation of the research routine did.
type ResearchAdvance struct {
	// Steps is how many steps this invocation finished.
	Steps int
	// Contended is true when another worker changed the run first, so this one stopped and left the run to it.
	Contended bool
}

// Limits on a research request; the handler and the engine enforce them through Normalize.
const (
	MaxResearchObjectiveRunes = 2000
	MaxResearchListItems      = 20
	MaxResearchItemRunes      = 500
	MaxResearchContextBytes   = 16 << 10
)

// Normalize validates the request and fills its defaults in place: depth and playbook must be known, text and
// lists are length-capped, and every budget is clamped to the ceiling BudgetMax. It returns an error wrapping
// ErrValidation for anything it will not accept.
func (r *ResearchRequest) Normalize() error {
	bad := func(msg string) error { return fmt.Errorf("%w: %s", ErrValidation, msg) }
	r.Objective = strings.TrimSpace(r.Objective)
	if r.Objective == "" {
		return bad("objective is required")
	}
	if len([]rune(r.Objective)) > MaxResearchObjectiveRunes {
		return bad(fmt.Sprintf("objective is longer than %d characters", MaxResearchObjectiveRunes))
	}
	if r.Depth == "" {
		r.Depth = DepthStandard
	}
	if !r.Depth.Valid() {
		return bad("depth must be one of quick, standard, deep, comprehensive")
	}
	if r.Playbook == "" {
		r.Playbook = "generic"
	}
	known := false
	for _, p := range ResearchPlaybooks {
		known = known || p == r.Playbook
	}
	if !known {
		return bad("playbook must be one of " + strings.Join(ResearchPlaybooks, ", "))
	}
	for _, l := range []struct {
		name  string
		items *[]string
	}{{"research_questions", &r.ResearchQuestions}, {"constraints", &r.Constraints}} {
		if len(*l.items) > MaxResearchListItems {
			return bad(fmt.Sprintf("%s has more than %d items", l.name, MaxResearchListItems))
		}
		clean := make([]string, 0, len(*l.items))
		for _, it := range *l.items {
			it = strings.TrimSpace(it)
			if it == "" {
				continue
			}
			if len([]rune(it)) > MaxResearchItemRunes {
				return bad(fmt.Sprintf("an item of %s is longer than %d characters", l.name, MaxResearchItemRunes))
			}
			clean = append(clean, it)
		}
		*l.items = clean
	}
	if len(r.Context) > 0 {
		b, err := json.Marshal(r.Context)
		if err != nil {
			return bad("context is not valid JSON")
		}
		if len(b) > MaxResearchContextBytes {
			return bad(fmt.Sprintf("context is larger than %d bytes", MaxResearchContextBytes))
		}
	}
	for name, v := range map[string]int{"max_searches": r.MaxSearches, "max_pages": r.MaxPages,
		"max_model_calls": r.MaxModelCalls, "max_time_seconds": r.MaxTimeSeconds} {
		if v < 0 {
			return bad(name + " cannot be negative")
		}
	}
	ceil := BudgetMax()
	r.MaxSearches = min(r.MaxSearches, ceil["max_searches"])
	r.MaxPages = min(r.MaxPages, ceil["max_pages"])
	r.MaxModelCalls = min(r.MaxModelCalls, ceil["max_model_calls"])
	r.MaxTimeSeconds = min(r.MaxTimeSeconds, ceil["max_time_seconds"])
	return nil
}

// ResearchDepth defines how thoroughly an objective is investigated.
type ResearchDepth string

const (
	DepthQuick         ResearchDepth = "quick"
	DepthStandard      ResearchDepth = "standard"
	DepthDeep          ResearchDepth = "deep"
	DepthComprehensive ResearchDepth = "comprehensive"
)

// Valid reports whether d is a known depth.
func (d ResearchDepth) Valid() bool {
	switch d {
	case DepthQuick, DepthStandard, DepthDeep, DepthComprehensive:
		return true
	}
	return false
}

// ResearchPlaybooks are the playbooks a run may name.
var ResearchPlaybooks = []string{"generic", "internship_watch", "company_intelligence", "alumni_intelligence"}

// ResearchPhase represents the state in the durable research state machine.
type ResearchPhase string

const (
	PhaseRequestReceived      ResearchPhase = "REQUEST_RECEIVED"
	PhaseContextLoading       ResearchPhase = "CONTEXT_LOADING"
	PhaseObjectiveAnalysis    ResearchPhase = "OBJECTIVE_ANALYSIS"
	PhasePlanning             ResearchPhase = "RESEARCH_PLANNING"
	PhaseSourceDiscovery      ResearchPhase = "SOURCE_DISCOVERY"
	PhaseInformationRetrieval ResearchPhase = "INFORMATION_RETRIEVAL"
	PhaseContentInspection    ResearchPhase = "CONTENT_INSPECTION"
	PhaseExtraction           ResearchPhase = "EXTRACTION"
	PhaseEvidenceValidation   ResearchPhase = "EVIDENCE_VALIDATION"
	PhaseGapAnalysis          ResearchPhase = "GAP_ANALYSIS"
	PhaseFollowUpResearch     ResearchPhase = "FOLLOW_UP_RESEARCH"
	PhaseSynthesis            ResearchPhase = "SYNTHESIS"
	PhasePersistence          ResearchPhase = "PERSISTENCE"
	PhaseCompleted            ResearchPhase = "COMPLETED"
	PhasePaused               ResearchPhase = "PAUSED"
	PhaseCancelled            ResearchPhase = "CANCELLED"
	PhaseFailed               ResearchPhase = "FAILED"
)

// IsTerminal reports whether the phase is a terminal state.
func (p ResearchPhase) IsTerminal() bool {
	return p == PhaseCompleted || p == PhaseCancelled || p == PhaseFailed
}

// ResearchQuestion is one question the research plan seeks to answer.
type ResearchQuestion struct {
	ID                    string      `json:"id"`
	Question              string      `json:"question"`
	Category              string      `json:"category,omitempty"`
	Status                string      `json:"status"` // pending, in_progress, answered, unresolved
	Answer                string      `json:"answer,omitempty"`
	SupportingEvidenceIDs []uuid.UUID `json:"supporting_evidence_ids,omitempty"`
	// FindingIDs are the supported findings that answer the question; a question with none is open.
	FindingIDs []string `json:"finding_ids,omitempty"`
}

// ResearchPlan is the structured execution plan generated by the planner.
type ResearchPlan struct {
	Questions         []ResearchQuestion `json:"questions"`
	InitialQueries    []string           `json:"initial_queries"`
	TargetSourceTypes []string           `json:"target_source_types,omitempty"`
	EstimatedRounds   int                `json:"estimated_rounds,omitempty"`
}

// ResearchFinding is one claim a model extracted from a page. It is "supported" only when its quote was found in
// the fetched text; otherwise it is "unverified". A finding whose value differs from another source's for the same
// subject and property is "conflicting".
type ResearchFinding struct {
	ID              string         `json:"id"`
	Topic           string         `json:"topic"`
	Claim           string         `json:"claim"`
	ExtractedValue  string         `json:"extracted_value,omitempty"`
	Quote           string         `json:"quote"`
	EvidenceID      *uuid.UUID     `json:"evidence_id,omitempty"`
	SourceURL       string         `json:"source_url,omitempty"`
	Confidence      int            `json:"confidence"`
	IsContradiction bool           `json:"is_contradiction,omitempty"`
	Status          EvidenceStatus `json:"status"`
	QuestionID      string         `json:"question_id,omitempty"`
	Subject         string         `json:"subject,omitempty"`
	Property        string         `json:"property,omitempty"`
}

// ResearchRequest holds the generic configuration for starting a research run.
type ResearchRequest struct {
	Playbook          string         `json:"playbook,omitempty"` // generic, internship_watch, company_intelligence, alumni_intelligence
	Objective         string         `json:"objective"`
	Context           map[string]any `json:"context,omitempty"`
	Constraints       []string       `json:"constraints,omitempty"`
	ResearchQuestions []string       `json:"research_questions,omitempty"`
	SourcePreferences []string       `json:"source_preferences,omitempty"`
	SourceExclusions  []string       `json:"source_exclusions,omitempty"`
	Depth             ResearchDepth  `json:"depth"`
	MaxSearches       int            `json:"max_searches,omitempty"`
	MaxPages          int            `json:"max_pages,omitempty"`
	MaxModelCalls     int            `json:"max_model_calls,omitempty"`
	MaxTimeSeconds    int            `json:"max_time_seconds,omitempty"`
	OutputSchema      map[string]any `json:"output_schema,omitempty"`
	StopConditions    []string       `json:"stop_conditions,omitempty"`
}

// ResearchRun tracks the state, progress and results of one durable research run.
type ResearchRun struct {
	ID                  uuid.UUID         `json:"id"`
	UserID              uuid.UUID         `json:"user_id"`
	Playbook            string            `json:"playbook"`
	Objective           string            `json:"objective"`
	Depth               ResearchDepth     `json:"depth"`
	Status              ResearchPhase     `json:"status"`
	Plan                ResearchPlan      `json:"plan"`
	ContextData         map[string]any    `json:"context_data"`
	ConstraintsList     []string          `json:"constraints_list"`
	BudgetConfig        map[string]int    `json:"budget_config"`
	ProgressMetrics     map[string]int    `json:"progress_metrics"`
	Findings            []ResearchFinding `json:"findings"`
	UnresolvedQuestions []string          `json:"unresolved_questions"`
	ReportMarkdown      string            `json:"report_markdown"`
	ReportStructured    map[string]any    `json:"report_structured"`
	ErrorMessage        *string           `json:"error_message,omitempty"`
	// Resumable state: the run is a state machine that any worker can advance one step at a time.
	Round           int      `json:"round"`
	PendingQueries  []string `json:"pending_queries"`
	ExecutedQueries []string `json:"executed_queries"`
	PendingURLs     []string `json:"pending_urls"`
	SeenURLs        []string `json:"seen_urls"`
	StopReason      string   `json:"stop_reason"`
	NextStepIndex   int      `json:"next_step_index"`
	// Revision counts writes; a write made from a stale copy of the run is refused.
	Revision    int64      `json:"revision"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// ResearchStep tracks individual execution steps in a research run.
type ResearchStep struct {
	ID         uuid.UUID      `json:"id"`
	RunID      uuid.UUID      `json:"run_id"`
	UserID     uuid.UUID      `json:"user_id"`
	StepIndex  int            `json:"step_index"`
	Phase      ResearchPhase  `json:"phase"`
	Action     string         `json:"action"`
	InputData  map[string]any `json:"input_data"`
	OutputData map[string]any `json:"output_data"`
	DurationMs int            `json:"duration_ms"`
	Status     string         `json:"status"` // pending, done, failed (older rows: completed)
	CreatedAt  time.Time      `json:"created_at"`
}

// BudgetMax is the ceiling no request may raise a budget above: the comprehensive tier.
func BudgetMax() map[string]int { return BudgetDefaults(DepthComprehensive) }

// BudgetDefaults returns sane budgets based on research depth.
func BudgetDefaults(depth ResearchDepth) map[string]int {
	switch depth {
	case DepthQuick:
		return map[string]int{"max_searches": 3, "max_pages": 4, "max_model_calls": 4, "max_time_seconds": 60}
	case DepthDeep:
		return map[string]int{"max_searches": 12, "max_pages": 20, "max_model_calls": 25, "max_time_seconds": 300}
	case DepthComprehensive:
		return map[string]int{"max_searches": 25, "max_pages": 45, "max_model_calls": 50, "max_time_seconds": 600}
	default: // standard
		return map[string]int{"max_searches": 6, "max_pages": 10, "max_model_calls": 12, "max_time_seconds": 120}
	}
}
