package manager

import (
	"fmt"
	"sync"

	"github.com/griffin/core/internal/domain"
)

// Registry manages registered domain managers and enforces manager governance.
type Registry struct {
	mu       sync.RWMutex
	managers map[string]Manager
}

// NewDefaultRegistry initializes the registry with Griffin's 11 standard specialist managers.
func NewDefaultRegistry() *Registry {
	r := &Registry{
		managers: make(map[string]Manager),
	}
	for _, def := range standardManagerDefinitions {
		r.managers[def.ID] = NewStandardManager(def)
	}
	return r
}

// Get retrieves a manager by ID.
func (r *Registry) Get(id string) (Manager, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.managers[id]
	return m, ok
}

// List returns definitions for all currently registered managers.
func (r *Registry) List() []ManagerDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]ManagerDefinition, 0, len(r.managers))
	for _, m := range r.managers {
		list = append(list, m.Definition())
	}
	return list
}

// Register adds or updates a manager. Creating a new manager role requires governance validation.
func (r *Registry) Register(m Manager, userAuthorized bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	def := m.Definition()
	if def.ID == "" {
		return fmt.Errorf("%w: manager ID cannot be empty", domain.ErrValidation)
	}

	_, exists := r.managers[def.ID]
	if !exists && !userAuthorized {
		return fmt.Errorf("%w: creating new manager-level roles requires explicit user approval", domain.ErrForbidden)
	}

	r.managers[def.ID] = m
	return nil
}

// IsKnownManager checks whether a manager ID is registered.
func (r *Registry) IsKnownManager(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.managers[id]
	return ok
}

// standardManagerDefinitions defines the 11 official Griffin specialist domain managers.
var standardManagerDefinitions = []ManagerDefinition{
	{
		ID:          "career",
		Name:        "Career Manager",
		Description: "Coordinates job searches, resume tailoring, application tracking, and recruiter correspondence.",
		Domain:      "career",
		Responsibilities: []string{
			"Identify relevant job opportunities and internships",
			"Tailor resumes and cover letters for specific roles",
			"Draft correspondence to recruiters and hiring managers",
			"Track application lifecycles and interview pipelines",
		},
		SupportedTaskTypes: []string{
			"JOB_SEARCH",
			"RESUME_TAILORING",
			"COVER_LETTER_DRAFT",
			"APPLICATION_PREPARATION",
			"INTERVIEW_PREP",
		},
		RequiredContext: []string{
			"target_role",
			"user_resume",
			"work_history",
			"desired_location",
		},
		ExpectedOutputFormats: []string{
			"APPLICATION_PACKAGE_JSON",
			"RESUME_MARKDOWN",
			"COVER_LETTER_MARKDOWN",
		},
		VerificationRules: []string{
			"Ensure resume references existing verified experience",
			"Verify contact details and job URLs are valid",
			"Check that no unapproved application submission occurs",
		},
		ApprovalOperations: []string{
			domain.ActionTypeJobApply,
			domain.ActionTypeEmailSend,
		},
		AllowedCapabilities: []string{
			domain.CapabilityResearchRead,
			domain.CapabilityWebRead,
			domain.CapabilityEmailDraft,
			domain.CapabilityJobApply,
			domain.CapabilityEmailSend,
		},
		AvailableWorkers: []string{
			"career_researcher",
			"resume_tailorer",
			"application_drafter",
		},
	},
	{
		ID:          "research",
		Name:        "Research Manager",
		Description: "Coordinates deep literature reviews, primary source investigations, and report syntheses.",
		Domain:      "research",
		Responsibilities: []string{
			"Conduct multi-source web and academic investigations",
			"Extract empirical facts and verify citations",
			"Synthesize structured research documents with markdown tables",
		},
		SupportedTaskTypes: []string{
			"LITERATURE_REVIEW",
			"TECHNICAL_INVESTIGATION",
			"FACT_VERIFICATION",
			"BENCHMARK_ANALYSIS",
		},
		RequiredContext: []string{
			"research_query",
			"depth_level",
			"source_constraints",
		},
		ExpectedOutputFormats: []string{
			"RESEARCH_REPORT_MARKDOWN",
			"EVIDENCE_TABLE_JSON",
		},
		VerificationRules: []string{
			"Ensure all claims are cited with primary sources",
			"Verify no hallucinated URLs or false paper titles",
		},
		ApprovalOperations: []string{},
		AllowedCapabilities: []string{
			domain.CapabilityResearchRead,
			domain.CapabilityWebRead,
		},
		AvailableWorkers: []string{
			"web_researcher",
			"citation_auditor",
			"synthesis_writer",
		},
	},
	{
		ID:          "engineering",
		Name:        "Engineering Manager",
		Description: "Coordinates software architecture, codebase audits, bug fixes, refactoring, and code reviews.",
		Domain:      "engineering",
		Responsibilities: []string{
			"Inspect local git repositories and directory structures",
			"Run static analysis, typechecking, and test suites",
			"Author robust code changes following repository conventions",
			"Generate clean git diffs and code review summaries",
		},
		SupportedTaskTypes: []string{
			"CODE_REVIEW",
			"BUG_DIAGNOSIS",
			"FEATURE_IMPLEMENTATION",
			"REFACTORING",
			"TEST_SUITE_EXPANSION",
		},
		RequiredContext: []string{
			"repository_path",
			"issue_description",
			"coding_standards",
		},
		ExpectedOutputFormats: []string{
			"CODE_DIFF",
			"REVIEW_REPORT_MARKDOWN",
			"TEST_RESULTS_JSON",
		},
		VerificationRules: []string{
			"Verify all new tests pass with zero regression",
			"Ensure pure argv argument handling and process safety",
		},
		ApprovalOperations: []string{
			domain.ActionTypeRepoModify,
		},
		AllowedCapabilities: []string{
			domain.CapabilityRepoRead,
			domain.CapabilityRepoWrite,
			domain.CapabilityResearchRead,
		},
		AvailableWorkers: []string{
			"code_reviewer",
			"bug_diagnostician",
			"test_engineer",
		},
	},
	{
		ID:          "hackathon",
		Name:        "Hackathon and Ecosystem Manager",
		Description: "Coordinates hackathon discovery, project brainstorming, submission prep, and grant applications.",
		Domain:      "hackathon",
		Responsibilities: []string{
			"Discover active AI/web3 hackathons and grant programs",
			"Evaluate bounty criteria, prizes, and deadlines",
			"Prepare project submission decks, videos, and readmes",
		},
		SupportedTaskTypes: []string{
			"HACKATHON_DISCOVERY",
			"BOUNTY_EVALUATION",
			"SUBMISSION_PREPARATION",
			"PITCH_DECK_OUTLINE",
		},
		RequiredContext: []string{
			"focus_areas",
			"team_skills",
			"timeline_constraints",
		},
		ExpectedOutputFormats: []string{
			"HACKATHON_LIST_JSON",
			"SUBMISSION_READINESS_MARKDOWN",
		},
		VerificationRules: []string{
			"Verify deadlines are strictly in the future",
			"Ensure prize pools and submission guidelines are accurate",
		},
		ApprovalOperations: []string{
			domain.ActionTypeJobApply,
		},
		AllowedCapabilities: []string{
			domain.CapabilityWebRead,
			domain.CapabilityResearchRead,
		},
		AvailableWorkers: []string{
			"hackathon_scout",
			"pitch_drafter",
		},
	},
	{
		ID:          "freelance",
		Name:        "Freelance Manager",
		Description: "Coordinates client proposals, contract gig searches, milestone tracking, and deliverables.",
		Domain:      "freelance",
		Responsibilities: []string{
			"Scout high-value freelance engineering and consulting gigs",
			"Draft tailored project proposals and budget estimates",
			"Track contract milestones and client deliverables",
		},
		SupportedTaskTypes: []string{
			"GIG_SCOUTING",
			"PROPOSAL_DRAFTING",
			"STATEMENT_OF_WORK",
			"MILESTONE_AUDIT",
		},
		RequiredContext: []string{
			"rate_card",
			"portfolio_links",
			"client_requirements",
		},
		ExpectedOutputFormats: []string{
			"PROPOSAL_MARKDOWN",
			"SOW_DOCUMENT_JSON",
		},
		VerificationRules: []string{
			"Confirm rate card minimums are strictly respected",
			"Verify scope definitions have unambiguous boundary conditions",
		},
		ApprovalOperations: []string{
			domain.ActionTypeEmailSend,
		},
		AllowedCapabilities: []string{
			domain.CapabilityWebRead,
			domain.CapabilityEmailDraft,
			domain.CapabilityResearchRead,
			domain.CapabilityEmailSend,
		},
		AvailableWorkers: []string{
			"proposal_writer",
			"gig_scout",
		},
	},
	{
		ID:          "networking",
		Name:        "Networking Manager",
		Description: "Coordinates relationship tracking, contact updates, coffee chat prep, and outbound drafting.",
		Domain:      "networking",
		Responsibilities: []string{
			"Maintain personal CRM contacts and interaction logs",
			"Draft warm outreach and follow-up messages",
			"Prepare background briefing sheets before networking meetings",
		},
		SupportedTaskTypes: []string{
			"CONTACT_BRIEFING",
			"OUTREACH_DRAFTING",
			"FOLLOW_UP_SCHEDULING",
			"CRM_UPDATE",
		},
		RequiredContext: []string{
			"contact_name",
			"contact_profile",
			"interaction_history",
		},
		ExpectedOutputFormats: []string{
			"MEETING_BRIEF_MARKDOWN",
			"OUTREACH_MESSAGE_TEXT",
		},
		VerificationRules: []string{
			"Ensure respectful, professional tone",
			"Verify no emails are sent without explicit approval",
		},
		ApprovalOperations: []string{
			domain.ActionTypeEmailSend,
		},
		AllowedCapabilities: []string{
			domain.CapabilityWebRead,
			domain.CapabilityEmailDraft,
			domain.CapabilityEmailSend,
		},
		AvailableWorkers: []string{
			"contact_researcher",
			"outreach_drafter",
		},
	},
	{
		ID:          "tech_intelligence",
		Name:        "Tech Intelligence Manager",
		Description: "Coordinates AI ecosystem monitoring, paper summaries, release tracking, and competitive intelligence.",
		Domain:      "tech_intelligence",
		Responsibilities: []string{
			"Track AI model releases (Gemini, Claude, OpenAI, Open Source)",
			"Summarize breakthrough research papers and benchmark shifts",
			"Produce morning executive briefings for user",
		},
		SupportedTaskTypes: []string{
			"AI_NEWS_DIGEST",
			"MODEL_BENCHMARK_REPORT",
			"BREAKTHROUGH_SUMMARY",
			"COMPETITIVE_SCAN",
		},
		RequiredContext: []string{
			"topics_of_interest",
			"time_window_hours",
		},
		ExpectedOutputFormats: []string{
			"EXECUTIVE_BRIEFING_MARKDOWN",
			"TREND_ANALYSIS_JSON",
		},
		VerificationRules: []string{
			"Ensure news items are dated within the requested window",
			"Include direct links to source announcements or repos",
		},
		ApprovalOperations: []string{},
		AllowedCapabilities: []string{
			domain.CapabilityWebRead,
			domain.CapabilityResearchRead,
		},
		AvailableWorkers: []string{
			"ecosystem_monitor",
			"briefing_synthesizer",
		},
	},
	{
		ID:          "learning",
		Name:        "Learning Manager",
		Description: "Coordinates study roadmaps, tutorial generation, concept flashcards, and technical deep-dives.",
		Domain:      "learning",
		Responsibilities: []string{
			"Design structured multi-week learning curricula",
			"Generate interactive quizzes and spaced repetition flashcards",
			"Synthesize complex papers into progressive mental models",
		},
		SupportedTaskTypes: []string{
			"CURRICULUM_DESIGN",
			"CONCEPT_EXPLANATION",
			"QUIZ_GENERATION",
			"READING_LIST_CURATION",
		},
		RequiredContext: []string{
			"subject_matter",
			"current_skill_level",
			"target_skill_level",
		},
		ExpectedOutputFormats: []string{
			"CURRICULUM_MARKDOWN",
			"FLASHCARD_DECK_JSON",
		},
		VerificationRules: []string{
			"Verify prerequisites are logically sequenced",
			"Ensure exercises have accurate solution explanations",
		},
		ApprovalOperations: []string{},
		AllowedCapabilities: []string{
			domain.CapabilityResearchRead,
			domain.CapabilityWebRead,
		},
		AvailableWorkers: []string{
			"curriculum_architect",
			"quiz_master",
		},
	},
	{
		ID:          "personal_ops",
		Name:        "Personal Operations Manager",
		Description: "Coordinates daily scheduling, reminders, travel logistics, and personal administrative tasks.",
		Domain:      "personal_ops",
		Responsibilities: []string{
			"Synthesize daily agendas and calendar conflicts",
			"Draft travel itineraries and packing check-lists",
			"Prepare routine administrative forms and checklists",
		},
		SupportedTaskTypes: []string{
			"SCHEDULE_SYNTHESIS",
			"REMINDER_SETUP",
			"ITINERARY_PLANNING",
			"ADMIN_CHECKLIST",
		},
		RequiredContext: []string{
			"calendar_data",
			"current_timezone",
			"travel_dates",
		},
		ExpectedOutputFormats: []string{
			"AGENDA_MARKDOWN",
			"ITINERARY_JSON",
		},
		VerificationRules: []string{
			"Verify timezone offsets and schedule overlaps",
			"Ensure calendar edits require explicit confirmation",
		},
		ApprovalOperations: []string{
			domain.ActionTypeCalendarModify,
		},
		AllowedCapabilities: []string{
			domain.CapabilityCalendarRead,
			domain.CapabilityCalendarWrite,
			domain.CapabilityEmailDraft,
		},
		AvailableWorkers: []string{
			"schedule_coordinator",
			"itinerary_planner",
		},
	},
	{
		ID:          "finance",
		Name:        "Finance and Developer Benefits Manager",
		Description: "Coordinates cloud credit tracking, developer grants, SaaS subscription audits, and budget optimization.",
		Domain:      "finance",
		Responsibilities: []string{
			"Monitor AWS credit balances and burn rates",
			"Track active developer subscriptions (GitHub, Antigravity, domains)",
			"Identify free-tier opportunities and cost reduction options",
		},
		SupportedTaskTypes: []string{
			"CREDIT_AUDIT",
			"SUBSCRIPTION_REVIEW",
			"CLOUD_COST_ESTIMATION",
			"BENEFIT_DISCOVERY",
		},
		RequiredContext: []string{
			"cloud_provider",
			"monthly_spend",
			"active_subscriptions",
		},
		ExpectedOutputFormats: []string{
			"COST_BREAKDOWN_MARKDOWN",
			"BENEFITS_LEDGER_JSON",
		},
		VerificationRules: []string{
			"Ensure all financial actions require critical approval",
			"Verify arithmetic totals across line items",
		},
		ApprovalOperations: []string{
			domain.ActionTypeFinancialAction,
		},
		AllowedCapabilities: []string{
			domain.CapabilityResearchRead,
			domain.CapabilityWebRead,
			domain.CapabilityFinancialAction,
		},
		AvailableWorkers: []string{
			"cloud_cost_auditor",
			"benefit_scout",
		},
	},
	{
		ID:          "product_ideas",
		Name:        "Product and Ideas Manager",
		Description: "Coordinates feature brainstorms, user story specs, PRDs, domain models, and architecture design.",
		Domain:      "product_ideas",
		Responsibilities: []string{
			"Draft Product Requirement Documents (PRDs) and user stories",
			"Develop ubiquitous domain language and ER diagrams",
			"Evaluate product feasibility, market differentiation, and risks",
		},
		SupportedTaskTypes: []string{
			"PRD_CREATION",
			"FEATURE_BRAINSTORM",
			"DOMAIN_MODELING",
			"COMPETITIVE_ANALYSIS",
		},
		RequiredContext: []string{
			"product_vision",
			"target_audience",
			"technical_constraints",
		},
		ExpectedOutputFormats: []string{
			"PRD_MARKDOWN",
			"DOMAIN_SPEC_JSON",
		},
		VerificationRules: []string{
			"Verify PRD includes explicit non-goals and metrics",
			"Ensure domain models have clear boundary seams",
		},
		ApprovalOperations: []string{},
		AllowedCapabilities: []string{
			domain.CapabilityResearchRead,
			domain.CapabilityWebRead,
		},
		AvailableWorkers: []string{
			"product_architect",
			"domain_modeler",
		},
	},
}
