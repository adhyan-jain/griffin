package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// The second batch of tools: everything the owner can do on the Alumni, Outreach, Library, Devices, Settings,
// Research, Packages and Tasks pages. Each tool calls the same repository or service the HTTP handler calls. A
// tool that changes Griffin's own data is internal (with an undo where the change can be reversed); a permanent
// delete is external. Nothing here sends a message: a LinkedIn message is handed to the owner to send by hand.

// PeopleAPI is the person store the Alumni page uses (*repository.PeopleRepository satisfies it).
type PeopleAPI interface {
	List(ctx context.Context, userID uuid.UUID, f repository.PersonFilter) ([]*domain.Person, error)
	Get(ctx context.Context, userID, id uuid.UUID) (*domain.Person, error)
	Update(ctx context.Context, userID, id uuid.UUID, state *domain.ContactState, dnc *bool) (*domain.Person, error)
	MarkDrafted(ctx context.Context, userID, id uuid.UUID) (bool, error)
	ApplyVerification(ctx context.Context, userID, id uuid.UUID, v *domain.VerifiedFields) (*domain.Person, error)
}

// AlumniHolder puts an outreach draft on the Approvals page; approving it sends nothing (*service.TaskEngine).
type AlumniHolder interface {
	HoldAlumniOutreach(ctx context.Context, userID uuid.UUID, name string, payload json.RawMessage) (*domain.Approval, error)
}

// AlumniImporter imports the Alumni tracker list into person records (*service.AlumniPeople).
type AlumniImporter interface {
	Import(ctx context.Context, userID uuid.UUID) (repository.ImportResult, error)
}

// VITAPI is the VIT alumni campaign (*service.VITCampaignService satisfies it).
type VITAPI interface {
	BuildQueue(ctx context.Context, userID uuid.UUID) (*domain.VITQueueBuild, error)
	GetCampaignQueue(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.VITCampaignTarget, error)
	GetCampaignStats(ctx context.Context, userID uuid.UUID) (*domain.VITCampaignStats, error)
	UpdateTargetAction(ctx context.Context, userID, targetID uuid.UUID, action string) error
	GetLinkedInQueries() []domain.LinkedInSearchQuery
}

// OutreachAPI is the campaign store (*repository.OutreachRepository satisfies it).
type OutreachAPI interface {
	CreateCampaign(ctx context.Context, c *domain.OutreachCampaign) error
	ListCampaigns(ctx context.Context, userID uuid.UUID) ([]*domain.OutreachCampaign, error)
	GetCampaign(ctx context.Context, userID, id uuid.UUID) (*domain.OutreachCampaign, error)
	UpdateCampaign(ctx context.Context, userID, id uuid.UUID, status *string, limits *domain.OutreachLimits) (*domain.OutreachCampaign, error)
	UpsertTargets(ctx context.Context, userID, campaignID uuid.UUID, in []domain.OutreachTarget) (int, error)
	ListTargets(ctx context.Context, userID, campaignID uuid.UUID, status *domain.OutreachStatus) ([]*domain.OutreachTarget, error)
	UpdateTarget(ctx context.Context, userID, id uuid.UUID, u repository.TargetUpdate) (*domain.OutreachTarget, error)
	CampaignOf(ctx context.Context, userID, targetID uuid.UUID) (uuid.UUID, error)
	Counts(ctx context.Context, userID, campaignID uuid.UUID) (map[string]int, error)
	AwaitingReply(ctx context.Context, userID uuid.UUID) ([]repository.Awaiting, error)
	RecordReplyByHandle(ctx context.Context, userID uuid.UUID, handle, text string) (*domain.OutreachTarget, error)
	RecordReplyByName(ctx context.Context, userID uuid.UUID, name, text string) (*domain.OutreachTarget, error)
	ClaimNextToSend(ctx context.Context, userID, campaignID uuid.UUID, now time.Time) (*repository.SendClaim, error)
}

// CampaignDrafter writes a first message for one person (*service.OutreachDrafter).
type CampaignDrafter interface {
	Draft(ctx context.Context, c *domain.OutreachCampaign, t *domain.OutreachTarget) (string, error)
}

// LibraryAPI is the owner's files and lists. The wiring joins the artifact repository and the storage service.
type LibraryAPI interface {
	ListArtifacts(ctx context.Context, userID uuid.UUID, taskID *uuid.UUID) ([]*domain.Artifact, error)
	ReadText(ctx context.Context, userID, id uuid.UUID, maxBytes int64) (string, error)
	UpdateText(ctx context.Context, id, userID uuid.UUID, text string, appendMode bool, maxBytes int64) (*domain.Artifact, error)
	CreateList(ctx context.Context, userID uuid.UUID, title, content string) (*domain.Artifact, error)
	DeleteArtifact(ctx context.Context, id, userID uuid.UUID) error
}

// HostsAPI is the device service (*service.HostService satisfies it).
type HostsAPI interface {
	Overview(ctx context.Context, userID uuid.UUID) (*service.HostOverview, error)
	SetPaused(ctx context.Context, userID, id uuid.UUID, paused bool) error
	SetGranted(ctx context.Context, userID, id uuid.UUID, caps []string) error
	Revoke(ctx context.Context, userID, id uuid.UUID) error
}

// ProfileAPI is the settings store (*repository.PostgresProfileRepository satisfies it).
type ProfileAPI interface {
	ManagerName(ctx context.Context, userID uuid.UUID) (string, error)
	SetManagerName(ctx context.Context, userID uuid.UUID, name string) error
	InboundCallsEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	SetInboundCallsEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error
}

// AliasesAPI is the alias store (repository.AliasRepository).
type AliasesAPI interface {
	List(ctx context.Context, userID uuid.UUID) (map[string]string, error)
	Put(ctx context.Context, userID uuid.UUID, alias, target string) error
	Delete(ctx context.Context, userID uuid.UUID, alias string) (bool, error)
}

// PolicyReader reads the owner's permission policy (*service.PolicyService). The agent never writes it.
type PolicyReader interface {
	Get(ctx context.Context, userID uuid.UUID) (*service.PolicyView, error)
}

// NotificationsAPI reads the notification outbox (repository.NotificationRepository). It never acknowledges.
type NotificationsAPI interface {
	After(ctx context.Context, userID uuid.UUID, after int64, limit int) ([]*domain.Notification, error)
	LatestSeq(ctx context.Context, userID uuid.UUID) (int64, error)
}

// ResearchAPI joins the research engine and its store.
type ResearchAPI interface {
	StartRun(ctx context.Context, userID uuid.UUID, req domain.ResearchRequest) (*domain.ResearchRun, error)
	PauseRun(ctx context.Context, userID, runID uuid.UUID) error
	ResumeRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error)
	CancelRun(ctx context.Context, userID, runID uuid.UUID) error
	FailRun(ctx context.Context, userID, runID uuid.UUID, msg string) error
	ListRuns(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error)
	GetRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error)
	ListEvidenceForRun(ctx context.Context, userID, runID uuid.UUID) ([]*domain.EvidenceItem, error)
}

// PackagesAPI reads application packages (repository.PackageRepository).
type PackagesAPI interface {
	PackageFor(ctx context.Context, userID, listingID uuid.UUID) (*domain.ApplicationPackage, error)
	GetPackage(ctx context.Context, userID, id uuid.UUID) (*domain.ApplicationPackage, error)
}

// CompaniesAPI reads the companies page (*repository.OpportunityRepository).
type CompaniesAPI interface {
	ListCompanySummaries(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.CompanySummary, error)
	GetCompanyDetail(ctx context.Context, userID, id uuid.UUID) (*repository.CompanyDetail, error)
}

// TaskInfoAPI reads a task's history and the scorecard (the task and report repositories).
type TaskInfoAPI interface {
	GetEvents(ctx context.Context, taskID uuid.UUID) ([]*domain.TaskEvent, error)
	GetAttempts(ctx context.Context, taskID uuid.UUID) ([]*domain.TaskAttempt, error)
	Audit(ctx context.Context, userID, taskID uuid.UUID) ([]domain.AuditEntry, error)
	ScoreRows(ctx context.Context, userID uuid.UUID, since time.Time) ([]domain.ScoreRow, error)
}

// ArchiveAPI is the task archive (repository.TaskArchiver).
type ArchiveAPI interface {
	ArchiveTasks(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, at time.Time) (int64, error)
	UnarchiveTask(ctx context.Context, userID, id uuid.UUID) (int64, error)
	DeleteArchived(ctx context.Context, userID uuid.UUID) (int64, error)
}

// DsaAPI is the DSA coach (*service.DsaCoach).
type DsaAPI interface {
	Today(ctx context.Context, userID uuid.UUID) ([]*domain.DsaItem, error)
	Sync(ctx context.Context, userID uuid.UUID) ([]string, error)
	MarkSolved(ctx context.Context, userID uuid.UUID, slug string) error
}

// BriefAPI builds the daily brief (*service.Briefer).
type BriefAPI interface {
	Build(ctx context.Context, userID uuid.UUID) (domain.Brief, error)
}

// ApprovalEditor edits the text of a pending approval (*service.TaskEngine).
type ApprovalEditor interface {
	EditApproval(ctx context.Context, approvalID, userID uuid.UUID, text string) (*domain.Approval, error)
}

// MoreDeps are the services the second batch wraps. A nil dependency leaves its tools out.
type MoreDeps struct {
	People     PeopleAPI
	Holder     AlumniHolder
	Importer   AlumniImporter
	Profiles   service.OwnerProfileSource
	GradCutoff int

	VIT      VITAPI
	Outreach OutreachAPI
	Drafter  CampaignDrafter

	Library       LibraryAPI
	Hosts         HostsAPI
	Profile       ProfileAPI
	Aliases       AliasesAPI
	Policy        PolicyReader
	Notifications NotificationsAPI
	Research      ResearchAPI
	Packages      PackagesAPI
	Companies     CompaniesAPI
	TaskInfo      TaskInfoAPI
	Archive       ArchiveAPI
	DeleteTask    func(ctx context.Context, id uuid.UUID) error
	DSA           DsaAPI
	Brief         BriefAPI
	ApprovalEdit  ApprovalEditor

	// Read-only pages: each returns the rows its page shows.
	Assessments    func(ctx context.Context, userID uuid.UUID) ([]*domain.AssessmentRecord, error)
	DiscoveredJobs func(ctx context.Context, userID uuid.UUID, status string, limit int) ([]*domain.DiscoveredJob, error)
	InboundEvents  func(ctx context.Context, userID uuid.UUID, limit int, actionable bool) ([]*domain.InboundEvent, error)
	SourceHealth   func(ctx context.Context, userID uuid.UUID) ([]domain.SourceHealth, error)
}

func newMoreTools(d Deps) []*Tool {
	m := d.More
	var ts []*Tool
	if m.People != nil {
		ts = append(ts, listAlumni(m), showAlumnus(m), updateAlumnus(m), verifyAlumnus(m), draftAlumniMessage(m))
		if m.Holder != nil {
			ts = append(ts, fileAlumniOutreach(m))
		}
		if m.Importer != nil {
			ts = append(ts, importAlumni(m))
		}
	}
	if m.VIT != nil {
		ts = append(ts, vitCampaignStatus(m), buildVITQueue(m), updateVITTarget(m), linkedinSearchQueries(m))
	}
	if m.Outreach != nil {
		ts = append(ts, listCampaigns(m), listCampaignTargets(m), createCampaign(m), updateCampaign(m), addCampaignTargets(m),
			updateOutreachTarget(m), nextOutreachTarget(m), recordOutreachReply(m), awaitingReplies(m))
		if m.Drafter != nil {
			ts = append(ts, draftCampaignMessages(m))
		}
	}
	if m.Library != nil {
		ts = append(ts, listLibrary(m), readArtifactText(m), updateArtifactText(m), createList(m), deleteArtifact(m))
	}
	if m.Hosts != nil {
		ts = append(ts, listDevices(m), updateDevice(m), revokeDevice(m))
	}
	if m.Profile != nil {
		ts = append(ts, showSettings(m), setManagerName(m), setInboundCalls(m))
	}
	if m.Aliases != nil {
		ts = append(ts, setAlias(m), removeAlias(m))
	}
	if m.Notifications != nil {
		ts = append(ts, listNotifications(m))
	}
	if m.Research != nil {
		ts = append(ts, listResearch(m), showResearch(m), controlResearch(d))
		if d.Tasks != nil {
			ts = append(ts, startResearch(d))
		}
	}
	if m.Packages != nil {
		ts = append(ts, showPackage(m))
	}
	if m.Companies != nil {
		ts = append(ts, listCompanies(m), showCompany(m))
	}
	if m.TaskInfo != nil && d.Tasks != nil {
		ts = append(ts, taskHistory(d), scorecard(m))
	}
	if m.Archive != nil && d.Tasks != nil {
		ts = append(ts, archiveTasks(d))
	}
	if m.DeleteTask != nil && d.Tasks != nil {
		ts = append(ts, deleteTasks(d))
	}
	if m.DSA != nil {
		ts = append(ts, dsaToday(m), markDsaSolved(m), syncDsa(m))
	}
	if m.Brief != nil {
		ts = append(ts, dailyBrief(m))
	}
	if m.ApprovalEdit != nil {
		ts = append(ts, editApproval(m))
	}
	if m.Assessments != nil {
		ts = append(ts, listAssessments(m))
	}
	if m.DiscoveredJobs != nil {
		ts = append(ts, listDiscoveredJobs(m))
	}
	if m.InboundEvents != nil {
		ts = append(ts, listInboundEvents(m))
	}
	if m.SourceHealth != nil {
		ts = append(ts, sourcesHealth(m))
	}
	return ts
}

// ---- helpers ----

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// jsonLine shows one row as compact JSON, cut to n characters.
func jsonLine(v any, n int) string { return truncate(string(rawJSON(v)), n) }

func undoInto(data json.RawMessage, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return ErrNoUndo
	}
	return nil
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	return min(n, max)
}

func str1(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ---- alumni ----

func gradCutoff(m MoreDeps) int {
	if m.GradCutoff > 0 {
		return m.GradCutoff
	}
	return domain.DefaultGraduationCutoff
}

func personLine(m MoreDeps, p *domain.Person) string {
	p.GraduationEligibility = domain.EvaluateGraduationEligibility(p.GraduationYear, p.GraduationDate, domain.GraduationCutoffDate(gradCutoff(m)))
	return fmt.Sprintf("%s | %s | %s, %s | tier %s | %s | do_not_contact=%t | meets_cutoff=%t", p.ID, truncate(p.Name, 60), truncate(p.Company, 40), truncate(p.Title, 50),
		p.PriorityTier, p.ContactState, p.DoNotContact, p.GraduationEligibility == domain.CohortEligible)
}

func listAlumni(m MoreDeps) *Tool {
	return &Tool{
		Name: "list_alumni",
		Description: "List the people on the owner's Alumni page (VIT alumni Griffin knows), with ids. Filters combine with AND: search (name text), company, country, role_family (tech or operations), " +
			"priority_tier, contactability_status, grad_year_max (graduated this year or earlier), do_not_contact. Newest priority first.",
		Effect: EffectRead,
		Schema: obj(map[string]any{"search": str("part of a name"), "company": str("part of a company name"), "country": str("country"), "role_family": str("tech or operations"),
			"priority_tier": str("for example P1, P2, REVIEW"), "contactability_status": str("contactability status"), "grad_year_max": num("graduated in this year or earlier"),
			"do_not_contact": flag("only people with this do-not-contact value"), "limit": num("at most this many (default 30, max 100)")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				Search, Company, Country string
				RoleFamily               string `json:"role_family"`
				PriorityTier             string `json:"priority_tier"`
				ContactabilityStatus     string `json:"contactability_status"`
				GradYearMax              *int   `json:"grad_year_max"`
				DoNotContact             *bool  `json:"do_not_contact"`
				Limit                    int
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			if a.RoleFamily != "" && a.RoleFamily != domain.RoleFamilyTech && a.RoleFamily != domain.RoleFamilyOperations {
				return Output{}, errors.New("role_family must be tech or operations")
			}
			if a.GradYearMax != nil && (*a.GradYearMax < 1950 || *a.GradYearMax > 2100) {
				return Output{}, errors.New("grad_year_max must be a year")
			}
			f := repository.PersonFilter{Search: strings.TrimSpace(a.Search), Company: strings.TrimSpace(a.Company), Country: strings.TrimSpace(a.Country),
				RoleFamily: a.RoleFamily, PriorityTier: a.PriorityTier, ContactabilityStatus: a.ContactabilityStatus, GradYearMax: a.GradYearMax,
				DoNotContact: a.DoNotContact, Limit: clampLimit(a.Limit, 30, 100), SortByPriority: true}
			people, err := m.People.List(ctx, c.UserID, f)
			if err != nil {
				return Output{}, err
			}
			if len(people) == 0 {
				return Output{Text: "No people match."}, nil
			}
			var b strings.Builder
			var ids []string
			for _, p := range people {
				ids = append(ids, p.ID.String())
				b.WriteString(personLine(m, p) + "\n")
			}
			return Output{Text: fmt.Sprintf("%d people (id | name | company, title | tier | contact state | flags):\n%s", len(people), b.String()), IDs: ids}, nil
		},
	}
}

func showAlumnus(m MoreDeps) *Tool {
	return &Tool{
		Name:        "show_alumnus",
		Description: "Show one person from the Alumni page in detail (campus, graduation, ACM role, contact route, tier and why). Use an id from list_alumni.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"id": str("person id from list_alumni")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			p, err := m.People.Get(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			head := personLine(m, p)
			text := fmt.Sprintf("%s\ncampus=%q vit=%q graduation_year=%v acm=%q role=%q route=%q email=%q channel=%q contactability=%q\nrationale: %s\nprofile: %s",
				head, p.Campus, p.VITAffiliation, p.GraduationYear, p.ACMAffiliation, p.ACMRole, p.PublicContactRoute, p.PublicBusinessEmail, p.PreferredChannel,
				p.ContactabilityStatus, truncate(p.PriorityRationale, 400), str1(p.ProfileURL))
			return Output{Text: text, IDs: []string{p.ID.String()}}, nil
		},
	}
}

// ownerStates are the contact states the owner may set by hand (the same list as the Alumni handler): drafted and
// approved come only from the outreach and approval flow.
var ownerStates = []domain.ContactState{domain.ContactNotContacted, domain.ContactSentManually, domain.ContactReplied, domain.ContactOptedOut}

func updateAlumnus(m MoreDeps) *Tool {
	return &Tool{
		Name: "update_alumnus",
		Description: "Record what happened with a person on the Alumni page: contact_state (not_contacted, sent_manually, replied, opted_out) and/or do_not_contact. " +
			"Opting out also turns do_not_contact on. Griffin never sends the message: 'sent_manually' only records that the owner sent it by hand.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"id": str("person id from list_alumni"), "contact_state": str("not_contacted, sent_manually, replied or opted_out"), "do_not_contact": flag("block or unblock contact")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				ID           string
				ContactState *domain.ContactState `json:"contact_state"`
				DoNotContact *bool                `json:"do_not_contact"`
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			if a.ContactState == nil && a.DoNotContact == nil {
				return Output{}, errors.New("give contact_state or do_not_contact")
			}
			if a.ContactState != nil {
				if !slices.Contains(ownerStates, *a.ContactState) {
					return Output{}, errors.New("contact_state must be one of not_contacted, sent_manually, replied, opted_out")
				}
				if *a.ContactState == domain.ContactOptedOut && a.DoNotContact == nil {
					t := true
					a.DoNotContact = &t
				}
			}
			before, err := m.People.Get(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			p, err := m.People.Update(ctx, c.UserID, id, a.ContactState, a.DoNotContact)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: "Updated: " + personLine(m, p), Summary: fmt.Sprintf("Updated %s: contact state %s, do not contact %t", p.Name, p.ContactState, p.DoNotContact),
				UndoData: rawJSON(map[string]any{"id": id, "state": before.ContactState, "dnc": before.DoNotContact})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct {
				ID    uuid.UUID
				State domain.ContactState
				DNC   bool
			}
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if _, err := m.People.Update(ctx, user, u.ID, &u.State, &u.DNC); err != nil {
				return "", err
			}
			return "The person is back as they were.", nil
		},
	}
}

func verifyAlumnus(m MoreDeps) *Tool {
	claim := func(desc string) map[string]any { return map[string]any{"type": "object", "description": desc} }
	return &Tool{
		Name: "verify_alumnus",
		Description: "Record the owner's evidence about a person. Give any of: campus {value, source_url, excerpt}; graduation {year, date, source_url, excerpt}; acm {status, role, tenure, source_url, excerpt}; " +
			"contact_route {value, source_url}. Every claim needs an http(s) source_url and an excerpt. Only a non-LinkedIn source confirms a campus or an ACM role.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"id": str("person id from list_alumni"), "campus": claim("campus claim"), "graduation": claim("graduation claim"),
			"acm": claim("ACM claim"), "contact_route": claim("public contact route claim")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				ID string
				domain.PersonVerification
			}
			var wrap struct {
				ID           string                    `json:"id"`
				Campus       *domain.CampusClaim       `json:"campus"`
				Graduation   *domain.GraduationClaim   `json:"graduation"`
				ACM          *domain.ACMClaim          `json:"acm"`
				ContactRoute *domain.ContactRouteClaim `json:"contact_route"`
			}
			if err := decode(raw, &wrap); err != nil {
				return Output{}, err
			}
			a.ID = wrap.ID
			a.Campus, a.Graduation, a.ACM, a.ContactRoute = wrap.Campus, wrap.Graduation, wrap.ACM, wrap.ContactRoute
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			fields, err := a.PersonVerification.Resolve(c.Now)
			if err != nil {
				return Output{}, err
			}
			p, err := m.People.ApplyVerification(ctx, c.UserID, id, fields)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: "Verified: " + personLine(m, p), Summary: "Recorded the evidence for " + p.Name}, nil
		},
	}
}

func draftAlumniMessage(m MoreDeps) *Tool {
	return &Tool{
		Name: "draft_alumni_message",
		Description: "Write (without filing or sending anything) an evidence-backed draft message for a person on the Alumni page, in two variants. " +
			"Do-not-contact and opted-out people are refused. Use file_alumni_outreach to put a final draft on the Approvals page.",
		Effect: EffectRead,
		Schema: obj(map[string]any{"id": str("person id from list_alumni")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			p, err := m.People.Get(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			sender, err := service.OwnerProfileOrEmpty(ctx, m.Profiles, c.UserID)
			if err != nil {
				return Output{}, err
			}
			draft, err := service.GenerateVITAlumniDraft(p, sender)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: "Draft for " + p.Name + " (nothing was filed or sent):\n" + string(rawJSON(draft))}, nil
		},
	}
}

func fileAlumniOutreach(m MoreDeps) *Tool {
	return &Tool{
		Name: "file_alumni_outreach",
		Description: "Put the owner's final draft message for a person on the Approvals page and mark the person drafted. This SENDS NOTHING and Griffin never sends a LinkedIn message: " +
			"after approval the draft is handed to the owner to send by hand. channel is linkedin, email or other. A do-not-contact person is refused.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"id": str("person id from list_alumni"), "draft_text": str("the message text"), "channel": str("linkedin, email or other")}, "id", "draft_text", "channel"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				ID, Channel string
				DraftText   string `json:"draft_text"`
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			a.DraftText = strings.TrimSpace(a.DraftText)
			switch {
			case a.DraftText == "":
				return Output{}, errors.New("draft_text is required")
			case utf8.RuneCountInString(a.DraftText) > domain.MaxOutreachDraftRunes:
				return Output{}, fmt.Errorf("draft_text is longer than %d characters", domain.MaxOutreachDraftRunes)
			case !slices.Contains(domain.OutreachChannels, a.Channel):
				return Output{}, fmt.Errorf("channel must be one of %s", strings.Join(domain.OutreachChannels, ", "))
			}
			p, err := m.People.Get(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			if p.DoNotContact || p.ContactState == domain.ContactOptedOut {
				return Output{}, fmt.Errorf("%s is marked do not contact", p.Name)
			}
			payload := rawJSON(map[string]any{"alumni_id": id.String(), "name": p.Name, "profile_url": p.ProfileURL, "draft_text": a.DraftText, "channel": a.Channel, "sends_nothing": true})
			ap, err := m.Holder.HoldAlumniOutreach(ctx, c.UserID, p.Name, payload)
			if err != nil {
				return Output{}, err
			}
			ok, err := m.People.MarkDrafted(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			if !ok {
				return Output{}, fmt.Errorf("%s can no longer be contacted", p.Name)
			}
			return Output{Text: fmt.Sprintf("The draft for %s is on the Approvals page (approval %s). Nothing was sent; after approval it is handed to the owner to send by hand.", p.Name, ap.ID),
				Summary: "Filed a " + a.Channel + " draft for " + p.Name + " on the Approvals page (sends nothing)", IDs: []string{ap.ID.String()}}, nil
		},
	}
}

func importAlumni(m MoreDeps) *Tool {
	return &Tool{
		Name:        "import_alumni",
		Description: "Import the Alumni tracker list into person records. Safe to repeat: it changes nothing it already wrote.",
		Effect:      EffectInternal,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			res, err := m.Importer.Import(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			s := fmt.Sprintf("Alumni import: %d created, %d updated, %d skipped", res.Created, res.Updated, res.Skipped)
			return Output{Text: s + ".", Summary: s}, nil
		},
	}
}

// ---- the VIT alumni campaign ----

func vitCampaignStatus(m MoreDeps) *Tool {
	return &Tool{
		Name:        "vit_campaign_status",
		Description: "Show the VIT alumni campaign: live cohort statistics and the prioritised queue of people to message (ids for update_vit_target).",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"limit": num("queue length (default 10, max 100)")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Limit int }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			stats, err := m.VIT.GetCampaignStats(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			q, err := m.VIT.GetCampaignQueue(ctx, c.UserID, clampLimit(a.Limit, 10, 100))
			if err != nil {
				return Output{}, err
			}
			var b strings.Builder
			var ids []string
			for _, t := range q {
				ids = append(ids, t.ID.String())
				b.WriteString(jsonLine(t, 300) + "\n")
			}
			return Output{Text: "Stats: " + string(rawJSON(stats)) + "\nQueue (" + fmt.Sprint(len(q)) + "):\n" + b.String(), IDs: ids}, nil
		},
	}
}

func buildVITQueue(m MoreDeps) *Tool {
	return &Tool{
		Name:        "build_vit_queue",
		Description: "Turn the owner's people records into the VIT campaign's outreach queue. Sends nothing.",
		Effect:      EffectInternal,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			res, err := m.VIT.BuildQueue(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: "Queue built: " + string(rawJSON(res)), Summary: "Built the VIT campaign queue"}, nil
		},
	}
}

func updateVITTarget(m MoreDeps) *Tool {
	return &Tool{
		Name: "update_vit_target",
		Description: "Move one person in the VIT campaign queue: action approve (the owner approves the message; only when the owner asks in chat), sent_manually (the owner sent it by hand), " +
			"reply (they replied) or dnc (do not contact). Griffin sends nothing.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"id": str("target id from vit_campaign_status"), "action": str("approve, sent_manually, reply or dnc")}, "id", "action"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID, Action string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			a.Action = strings.ToLower(strings.TrimSpace(a.Action))
			if a.Action == "approve" && c.Origin != OriginOwnerChat {
				return Output{}, errors.New("only the owner can approve a message")
			}
			if err := m.VIT.UpdateTargetAction(ctx, c.UserID, id, a.Action); err != nil {
				return Output{}, err
			}
			return Output{Text: "Done: " + a.Action, Summary: "VIT campaign target " + id.String() + ": " + a.Action}, nil
		},
	}
}

func linkedinSearchQueries(m MoreDeps) *Tool {
	return &Tool{
		Name:        "linkedin_search_queries",
		Description: "Show the ready-made LinkedIn people-search queries (with links) for finding alumni by hand. Griffin never browses or messages on LinkedIn.",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var b strings.Builder
			for _, q := range m.VIT.GetLinkedInQueries() {
				fmt.Fprintf(&b, "%s | %s | %s\n", q.Category, truncate(q.Query, 120), q.URL)
			}
			return Output{Text: b.String()}, nil
		},
	}
}

// ---- outreach campaigns ----

func listCampaigns(m MoreDeps) *Tool {
	return &Tool{
		Name:        "list_campaigns",
		Description: "List the owner's outreach campaigns with their ids, status, channel and how many people are in each state.",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			cs, err := m.Outreach.ListCampaigns(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			if len(cs) == 0 {
				return Output{Text: "There are no outreach campaigns."}, nil
			}
			var b strings.Builder
			var ids []string
			for _, cp := range cs {
				ids = append(ids, cp.ID.String())
				counts, _ := m.Outreach.Counts(ctx, c.UserID, cp.ID)
				keys := make([]string, 0, len(counts))
				for k := range counts {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				var cn []string
				for _, k := range keys {
					cn = append(cn, fmt.Sprintf("%s=%d", k, counts[k]))
				}
				fmt.Fprintf(&b, "%s | %s | %s | %s | %s\n", cp.ID, truncate(cp.Name, 60), cp.Channel, cp.Status, strings.Join(cn, " "))
			}
			return Output{Text: fmt.Sprintf("%d campaigns (id | name | channel | status | counts):\n%s", len(cs), b.String()), IDs: ids}, nil
		},
	}
}

func listCampaignTargets(m MoreDeps) *Tool {
	return &Tool{
		Name: "list_campaign_targets",
		Description: "List the people in one outreach campaign with their ids, status and draft. Optional status filter: found, drafted, approved, sent, replied, followup_due, closed. " +
			"Use the ids with update_outreach_target.",
		Effect: EffectRead,
		Schema: obj(map[string]any{"campaign_id": str("campaign id from list_campaigns"), "status": str("optional status filter"), "limit": num("at most this many (default 40)")}, "campaign_id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				CampaignID string `json:"campaign_id"`
				Status     string
				Limit      int
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			cid, err := c.ParseSeenID(a.CampaignID)
			if err != nil {
				return Output{}, err
			}
			var st *domain.OutreachStatus
			if a.Status != "" {
				s := domain.OutreachStatus(a.Status)
				if !s.Valid() {
					return Output{}, errors.New("unknown status")
				}
				st = &s
			}
			ts, err := m.Outreach.ListTargets(ctx, c.UserID, cid, st)
			if err != nil {
				return Output{}, err
			}
			if len(ts) == 0 {
				return Output{Text: "No people match."}, nil
			}
			n := clampLimit(a.Limit, 40, 100)
			var b strings.Builder
			var ids []string
			for i, t := range ts {
				if i == n {
					break
				}
				ids = append(ids, t.ID.String())
				fmt.Fprintf(&b, "%s | %s | %s | %s | draft: %s\n", t.ID, truncate(t.Name, 60), truncate(t.Handle, 60), t.Status, truncate(t.Draft, 100))
			}
			return Output{Text: fmt.Sprintf("%d people (showing %d) (id | name | handle | status | draft):\n%s", len(ts), len(ids), b.String()), IDs: ids}, nil
		},
	}
}

var campaignChannels = []string{"email", "linkedin", "whatsapp", "instagram", "other"}

func createCampaign(m MoreDeps) *Tool {
	return &Tool{
		Name:        "create_campaign",
		Description: "Create an outreach campaign (a job like 'cold-mail 50 founders'). channel is email, linkedin, whatsapp, instagram or other (default email). Sending limits take the safe defaults for the channel.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"name": str("campaign name"), "goal": str("what it is for"), "channel": str("email, linkedin, whatsapp, instagram or other")}, "name"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Name, Goal, Channel string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			a.Name = strings.TrimSpace(a.Name)
			if a.Name == "" {
				return Output{}, errors.New("name is required")
			}
			if a.Channel == "" {
				a.Channel = "email"
			}
			if !slices.Contains(campaignChannels, a.Channel) {
				return Output{}, errors.New("channel must be email, linkedin, whatsapp, instagram or other")
			}
			cp := &domain.OutreachCampaign{UserID: c.UserID, Name: a.Name, Goal: a.Goal, Channel: a.Channel, Limits: domain.DefaultOutreachLimits(a.Channel)}
			if err := m.Outreach.CreateCampaign(ctx, cp); err != nil {
				return Output{}, err
			}
			return Output{Text: fmt.Sprintf("Created campaign %s (id %s).", cp.Name, cp.ID), Summary: "Created the " + cp.Channel + " campaign " + cp.Name, IDs: []string{cp.ID.String()},
				UndoData: rawJSON(map[string]any{"id": cp.ID, "status": "active"})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ ID uuid.UUID }
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			done := "done"
			if _, err := m.Outreach.UpdateCampaign(ctx, user, u.ID, &done, nil); err != nil {
				return "", err
			}
			return "The campaign is marked done (campaigns cannot be deleted).", nil
		},
	}
}

func updateCampaign(m MoreDeps) *Tool {
	return &Tool{
		Name:        "update_campaign",
		Description: "Pause, resume or finish an outreach campaign: status active, paused or done.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"id": str("campaign id from list_campaigns"), "status": str("active, paused or done")}, "id", "status"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID, Status string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			if a.Status != "active" && a.Status != "paused" && a.Status != "done" {
				return Output{}, errors.New("status must be active, paused or done")
			}
			before, err := m.Outreach.GetCampaign(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			prevStatus := before.Status
			cp, err := m.Outreach.UpdateCampaign(ctx, c.UserID, id, &a.Status, nil)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: fmt.Sprintf("Campaign %s is now %s.", cp.Name, cp.Status), Summary: fmt.Sprintf("Campaign %s set to %s", cp.Name, cp.Status),
				UndoData: rawJSON(map[string]any{"id": id, "status": prevStatus})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct {
				ID     uuid.UUID
				Status string
			}
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if _, err := m.Outreach.UpdateCampaign(ctx, user, u.ID, &u.Status, nil); err != nil {
				return "", err
			}
			return "The campaign is back to " + u.Status + ".", nil
		},
	}
}

func addCampaignTargets(m MoreDeps) *Tool {
	person := obj(map[string]any{"name": str("their name"), "handle": str("email address, profile URL or phone: unique in the campaign"), "info": str("a line about them")}, "name", "handle")
	return &Tool{
		Name:        "add_campaign_targets",
		Description: "Add people to an outreach campaign (1 to 100 at a time). A person already in the campaign keeps their status.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"campaign_id": str("campaign id from list_campaigns"), "people": map[string]any{"type": "array", "items": person, "description": "the people to add"}}, "campaign_id", "people"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				CampaignID string `json:"campaign_id"`
				People     []struct{ Name, Handle, Info string }
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			cid, err := c.ParseSeenID(a.CampaignID)
			if err != nil {
				return Output{}, err
			}
			if len(a.People) == 0 || len(a.People) > 100 {
				return Output{}, errors.New("give between 1 and 100 people at a time")
			}
			in := make([]domain.OutreachTarget, 0, len(a.People))
			for _, p := range a.People {
				if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Handle) == "" {
					return Output{}, errors.New("every person needs a name and a handle")
				}
				in = append(in, domain.OutreachTarget{Name: strings.TrimSpace(p.Name), Handle: strings.TrimSpace(p.Handle), Info: p.Info})
			}
			added, err := m.Outreach.UpsertTargets(ctx, c.UserID, cid, in)
			if err != nil {
				return Output{}, err
			}
			s := fmt.Sprintf("Added %d of %d people to the campaign", added, len(in))
			return Output{Text: s + ".", Summary: s}, nil
		},
	}
}

func draftCampaignMessages(m MoreDeps) *Tool {
	return &Tool{
		Name:        "draft_campaign_messages",
		Description: "Write a first message for up to ten people in a campaign who are still only 'found', and move them to 'drafted' for the owner to approve. Call again for the next ten. Sends nothing.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"campaign_id": str("campaign id from list_campaigns")}, "campaign_id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				CampaignID string `json:"campaign_id"`
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			cid, err := c.ParseSeenID(a.CampaignID)
			if err != nil {
				return Output{}, err
			}
			cp, err := m.Outreach.GetCampaign(ctx, c.UserID, cid)
			if err != nil {
				return Output{}, err
			}
			found := domain.OutreachFound
			people, err := m.Outreach.ListTargets(ctx, c.UserID, cid, &found)
			if err != nil {
				return Output{}, err
			}
			if len(people) > 10 {
				people = people[:10]
			}
			to := domain.OutreachDrafted
			drafted, failed := 0, 0
			for _, t := range people {
				text, err := m.Drafter.Draft(ctx, cp, t)
				if errors.Is(err, service.ErrDrafterOff) {
					return Output{}, err
				}
				if err != nil {
					failed++
					continue
				}
				if _, err := m.Outreach.UpdateTarget(ctx, c.UserID, t.ID, repository.TargetUpdate{To: &to, Draft: &text, Event: "agent: drafted"}); err != nil {
					failed++
					continue
				}
				drafted++
			}
			s := fmt.Sprintf("Drafted %d message(s) for %s (%d failed)", drafted, cp.Name, failed)
			return Output{Text: s + ". Nothing was sent.", Summary: s}, nil
		},
	}
}

func findTarget(ctx context.Context, m MoreDeps, user, id uuid.UUID) (*domain.OutreachTarget, error) {
	cid, err := m.Outreach.CampaignOf(ctx, user, id)
	if err != nil {
		return nil, err
	}
	ts, err := m.Outreach.ListTargets(ctx, user, cid, nil)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, domain.ErrNotFound
}

func updateOutreachTarget(m MoreDeps) *Tool {
	return &Tool{
		Name: "update_outreach_target",
		Description: "Edit one person in an outreach campaign: draft text, notes, reply_text, and/or move status (found > drafted > approved > sent > replied > closed; followup_due after sent). Illegal moves are refused. " +
			"'approved' only when the owner asks in chat. 'sent' only records that the owner sent the message by hand: Griffin never sends it, and never sends LinkedIn messages.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"id": str("person id from list_campaign_targets"), "status": str("new status"), "draft": str("new draft text (max 8000 characters)"),
			"notes": str("notes"), "reply_text": str("what they replied")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				ID        string
				Status    *domain.OutreachStatus
				Draft     *string
				Notes     *string
				ReplyText *string `json:"reply_text"`
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			if a.Status == nil && a.Draft == nil && a.Notes == nil && a.ReplyText == nil {
				return Output{}, errors.New("nothing to change")
			}
			if a.Status != nil && !a.Status.Valid() {
				return Output{}, errors.New("unknown status")
			}
			if a.Status != nil && *a.Status == domain.OutreachApproved && c.Origin != OriginOwnerChat {
				return Output{}, errors.New("only the owner can approve a message")
			}
			if a.Draft != nil && len([]rune(*a.Draft)) > 8000 {
				return Output{}, errors.New("draft is too long")
			}
			before, err := findTarget(ctx, m, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			prevDraft, prevNotes, prevReply := before.Draft, before.Notes, before.ReplyText
			u := repository.TargetUpdate{To: a.Status, Draft: a.Draft, Notes: a.Notes, ReplyText: a.ReplyText}
			switch {
			case a.Status != nil:
				u.Event = "owner: " + string(*a.Status)
			case a.Draft != nil:
				u.Event = "owner: edited draft"
			}
			if a.Status != nil && *a.Status == domain.OutreachSent {
				cid, err := m.Outreach.CampaignOf(ctx, c.UserID, id)
				if err != nil {
					return Output{}, err
				}
				cp, err := m.Outreach.GetCampaign(ctx, c.UserID, cid)
				if err != nil {
					return Output{}, err
				}
				next := c.Now.UTC().Add(time.Duration(cp.Limits.FollowupAfterDay) * 24 * time.Hour)
				u.NextFollowupAt = &next
			}
			t, err := m.Outreach.UpdateTarget(ctx, c.UserID, id, u)
			if err != nil {
				return Output{}, err
			}
			out := Output{Text: fmt.Sprintf("%s is now %s.", t.Name, t.Status), Summary: fmt.Sprintf("Updated %s in the campaign (status %s)", t.Name, t.Status)}
			if a.Status == nil { // a status move is one-way (the state machine), so only text edits can be put back
				out.UndoData = rawJSON(map[string]any{"id": id, "draft": prevDraft, "notes": prevNotes, "reply": prevReply,
					"set_draft": a.Draft != nil, "set_notes": a.Notes != nil, "set_reply": a.ReplyText != nil})
			}
			return out, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var w struct {
				ID       uuid.UUID `json:"id"`
				Draft    string    `json:"draft"`
				Notes    string    `json:"notes"`
				Reply    string    `json:"reply"`
				SetDraft bool      `json:"set_draft"`
				SetNotes bool      `json:"set_notes"`
				SetReply bool      `json:"set_reply"`
			}
			if err := undoInto(data, &w); err != nil {
				return "", err
			}
			up := repository.TargetUpdate{Event: "owner: undid an edit"}
			if w.SetDraft {
				up.Draft = &w.Draft
			}
			if w.SetNotes {
				up.Notes = &w.Notes
			}
			if w.SetReply {
				up.ReplyText = &w.Reply
			}
			if _, err := m.Outreach.UpdateTarget(ctx, user, w.ID, up); err != nil {
				return "", err
			}
			return "The edit was put back.", nil
		},
	}
}

func nextOutreachTarget(m MoreDeps) *Tool {
	return &Tool{
		Name: "next_outreach_target",
		Description: "Answer 'who should I message next?' for a campaign, within its daily limits and quiet hours (IST). It hands the next approved person to the owner to message by hand and holds them for five minutes. " +
			"Griffin sends nothing.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"campaign_id": str("campaign id from list_campaigns")}, "campaign_id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				CampaignID string `json:"campaign_id"`
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			cid, err := c.ParseSeenID(a.CampaignID)
			if err != nil {
				return Output{}, err
			}
			claim, err := m.Outreach.ClaimNextToSend(ctx, c.UserID, cid, c.Now.In(ist))
			if err != nil {
				return Output{}, err
			}
			if claim == nil || claim.Target == nil {
				why := "nobody is due right now"
				if claim != nil && claim.Reason != "" {
					why = claim.Reason
				}
				return Output{Text: "No one to message: " + why + "."}, nil
			}
			t := claim.Target
			return Output{Text: fmt.Sprintf("Next: %s (%s). Draft:\n%s\nThe owner sends it by hand, then marks them sent.", t.Name, t.Handle, t.Draft),
				Summary: "Picked " + t.Name + " as the next person to message by hand", IDs: []string{t.ID.String()}}, nil
		},
	}
}

func recordOutreachReply(m MoreDeps) *Tool {
	return &Tool{
		Name:        "record_outreach_reply",
		Description: "Record that someone replied: give their name (or handle) and optionally what they said. Moves the matching person to 'replied'.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"name": str("their name"), "handle": str("their email, profile URL or phone"), "text": str("what they said")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Name, Handle, Text string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			var t *domain.OutreachTarget
			var err error
			switch {
			case strings.TrimSpace(a.Handle) != "":
				t, err = m.Outreach.RecordReplyByHandle(ctx, c.UserID, strings.TrimSpace(a.Handle), a.Text)
			case strings.TrimSpace(a.Name) != "":
				t, err = m.Outreach.RecordReplyByName(ctx, c.UserID, strings.TrimSpace(a.Name), a.Text)
			default:
				return Output{}, errors.New("give a name or a handle")
			}
			if err != nil {
				return Output{}, err
			}
			if t == nil {
				return Output{Text: "Nobody in a campaign matches. Nothing was recorded."}, nil
			}
			return Output{Text: t.Name + " is marked replied.", Summary: "Recorded a reply from " + t.Name}, nil
		},
	}
}

func awaitingReplies(m MoreDeps) *Tool {
	return &Tool{
		Name:        "awaiting_replies",
		Description: "List the people who were messaged and have not answered yet (names and addresses).",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			list, err := m.Outreach.AwaitingReply(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			if len(list) == 0 {
				return Output{Text: "Nobody is awaiting a reply."}, nil
			}
			var b strings.Builder
			for i, w := range list {
				if i == 50 {
					break
				}
				b.WriteString(jsonLine(w, 200) + "\n")
			}
			return Output{Text: fmt.Sprintf("%d awaiting a reply:\n%s", len(list), b.String())}, nil
		},
	}
}

// ---- library ----

const maxLibraryText = 5 << 20

func listLibrary(m MoreDeps) *Tool {
	return &Tool{
		Name:        "list_library",
		Description: "List the owner's Library files and lists (not the vault) with ids, titles and types. Use the ids with read_artifact_text and update_artifact_text.",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			arts, err := m.Library.ListArtifacts(ctx, c.UserID, nil)
			if err != nil {
				return Output{}, err
			}
			var b strings.Builder
			var ids []string
			for _, a := range arts {
				if _, isVault := domain.ParseVaultMeta(a.Metadata); isVault {
					continue
				}
				if len(ids) == 100 {
					break
				}
				ids = append(ids, a.ID.String())
				fmt.Fprintf(&b, "%s | %s | %s\n", a.ID, truncate(a.Title, 80), a.ArtifactType)
			}
			if len(ids) == 0 {
				return Output{Text: "The Library is empty."}, nil
			}
			return Output{Text: fmt.Sprintf("%d files (id | title | type):\n%s", len(ids), b.String()), IDs: ids}, nil
		},
	}
}

func readArtifactText(m MoreDeps) *Tool {
	return &Tool{
		Name:        "read_artifact_text",
		Description: "Read the text of one Library file or list (Markdown or plain text). Long text continues with read_more.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"id": str("file id from list_library")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			text, err := m.Library.ReadText(ctx, c.UserID, id, maxLibraryText)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: text}, nil
		},
	}
}

func updateArtifactText(m MoreDeps) *Tool {
	return &Tool{
		Name:        "update_artifact_text",
		Description: "Change the text of a Library file or list: mode replace (default) overwrites it, mode append adds to the end. The owner can undo.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"id": str("file id from list_library"), "text": str("the text"), "mode": str("replace or append")}, "id", "text"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID, Text, Mode string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			if a.Mode != "" && a.Mode != "replace" && a.Mode != "append" {
				return Output{}, errors.New("mode must be replace or append")
			}
			if a.Mode == "append" && strings.TrimSpace(a.Text) == "" {
				return Output{}, errors.New("nothing to append")
			}
			prior, perr := m.Library.ReadText(ctx, c.UserID, id, maxLibraryText)
			art, err := m.Library.UpdateText(ctx, id, c.UserID, a.Text, a.Mode == "append", maxLibraryText)
			if err != nil {
				return Output{}, err
			}
			out := Output{Text: "Updated " + art.Title + ".", Summary: "Updated the text of " + art.Title}
			if perr == nil && len(prior) <= 200<<10 {
				out.UndoData = rawJSON(map[string]any{"id": id, "text": prior})
			}
			return out, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct {
				ID   uuid.UUID
				Text string
			}
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if _, err := m.Library.UpdateText(ctx, u.ID, user, u.Text, false, maxLibraryText); err != nil {
				return "", err
			}
			return "The text is back as it was.", nil
		},
	}
}

func createList(m MoreDeps) *Tool {
	return &Tool{
		Name:        "create_list",
		Description: "Create a new editable list (a Markdown file) in the Library with a title and optional starting content.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"title": str("list title"), "content": str("starting Markdown content")}, "title"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Title, Content string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			if strings.TrimSpace(a.Title) == "" {
				return Output{}, errors.New("a list needs a title")
			}
			art, err := m.Library.CreateList(ctx, c.UserID, strings.TrimSpace(a.Title), a.Content)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: fmt.Sprintf("Created the list %s (id %s).", art.Title, art.ID), Summary: "Created the list " + art.Title, IDs: []string{art.ID.String()},
				UndoData: rawJSON(map[string]any{"id": art.ID})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ ID uuid.UUID }
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if err := m.Library.DeleteArtifact(ctx, u.ID, user); err != nil {
				return "", err
			}
			return "The new list was removed.", nil
		},
	}
}

type idPayload struct {
	ID uuid.UUID `json:"id"`
}

func deleteArtifact(m MoreDeps) *Tool {
	return &Tool{
		Name:        "delete_artifact",
		Description: "Permanently delete one Library file or list (cannot be undone). When a routine started this, it waits for the owner's approval.",
		Effect:      EffectExternal,
		Schema:      obj(map[string]any{"id": str("file id from list_library")}, "id"),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			id, err := uuid.Parse(strings.TrimSpace(a.ID))
			if err != nil {
				return nil, "", errors.New("id is not a valid id")
			}
			return rawJSON(idPayload{id}), "Delete the Library file " + id.String(), nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			var p idPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				return "", err
			}
			if err := m.Library.DeleteArtifact(ctx, p.ID, user); err != nil {
				return "", err
			}
			return "Deleted the Library file " + p.ID.String() + ".", nil
		},
	}
}

// ---- devices ----

func listDevices(m MoreDeps) *Tool {
	return &Tool{
		Name:        "list_devices",
		Description: "List the owner's devices (laptop, phone, servers) with ids, whether each is live or paused, and which capabilities are granted and effective, plus the tasks waiting for a device.",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			ov, err := m.Hosts.Overview(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			var b strings.Builder
			var ids []string
			for _, h := range ov.Hosts {
				if h.Host == nil {
					continue
				}
				ids = append(ids, h.ID.String())
				fmt.Fprintf(&b, "%s | %s | %s | live=%t paused=%t revoked=%t | granted=%v effective=%v\n", h.ID, h.Name, h.Kind, h.Live, h.Paused, h.RevokedAt != nil, h.Capabilities, h.Effective)
			}
			return Output{Text: fmt.Sprintf("Mode %v. %d devices (id | name | kind | state | capabilities); %d tasks waiting:\n%s", ov.Mode, len(ids), len(ov.Waiting), b.String()), IDs: ids}, nil
		},
	}
}

func hostByID(ctx context.Context, m MoreDeps, user, id uuid.UUID) (*domain.Host, error) {
	ov, err := m.Hosts.Overview(ctx, user)
	if err != nil {
		return nil, err
	}
	for _, h := range ov.Hosts {
		if h.Host != nil && h.ID == id {
			return h.Host, nil
		}
	}
	return nil, domain.ErrNotFound
}

func updateDevice(m MoreDeps) *Tool {
	return &Tool{
		Name: "update_device",
		Description: "Pause or resume a device (paused true or false) and/or replace the capabilities the owner grants it (capabilities: the full list to grant; an empty list grants nothing). " +
			"To grant one more, give the current list plus the new one from list_devices.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"id": str("device id from list_devices"), "paused": flag("true to pause, false to resume"), "capabilities": strs("the complete list of granted capabilities")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				ID           string
				Paused       *bool
				Capabilities *[]string
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			if a.Paused == nil && a.Capabilities == nil {
				return Output{}, errors.New("give paused or capabilities")
			}
			before, err := hostByID(ctx, m, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			prevPaused, prevCaps, hostName := before.Paused, slices.Clone(before.Capabilities), before.Name
			if a.Paused != nil {
				if err := m.Hosts.SetPaused(ctx, c.UserID, id, *a.Paused); err != nil {
					return Output{}, err
				}
			}
			if a.Capabilities != nil {
				if err := m.Hosts.SetGranted(ctx, c.UserID, id, *a.Capabilities); err != nil {
					return Output{}, err
				}
			}
			return Output{Text: "Device " + hostName + " updated.", Summary: "Updated the device " + hostName,
				UndoData: rawJSON(map[string]any{"id": id, "paused": prevPaused, "caps": prevCaps, "set_paused": a.Paused != nil, "set_caps": a.Capabilities != nil})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct {
				ID        uuid.UUID `json:"id"`
				Paused    bool      `json:"paused"`
				Caps      []string  `json:"caps"`
				SetPaused bool      `json:"set_paused"`
				SetCaps   bool      `json:"set_caps"`
			}
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if u.SetPaused {
				if err := m.Hosts.SetPaused(ctx, user, u.ID, u.Paused); err != nil {
					return "", err
				}
			}
			if u.SetCaps {
				if err := m.Hosts.SetGranted(ctx, user, u.ID, u.Caps); err != nil {
					return "", err
				}
			}
			return "The device is back as it was.", nil
		},
	}
}

func revokeDevice(m MoreDeps) *Tool {
	return &Tool{
		Name:        "revoke_device",
		Description: "Permanently disable a device's access token (it must be registered again to come back). When a routine started this, it waits for the owner's approval.",
		Effect:      EffectExternal,
		Schema:      obj(map[string]any{"id": str("device id from list_devices")}, "id"),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			id, err := uuid.Parse(strings.TrimSpace(a.ID))
			if err != nil {
				return nil, "", errors.New("id is not a valid id")
			}
			return rawJSON(idPayload{id}), "Revoke the device " + id.String(), nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			var p idPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				return "", err
			}
			if err := m.Hosts.Revoke(ctx, user, p.ID); err != nil {
				return "", err
			}
			return "Revoked the device " + p.ID.String() + ".", nil
		},
	}
}

// ---- settings and aliases ----

func showSettings(m MoreDeps) *Tool {
	return &Tool{
		Name:        "show_settings",
		Description: "Show the owner's settings: the assistant's name, whether inbound calls are on, the aliases ('mummy' means 'mom'), and the permission policy (read only: the agent never changes permissions).",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			name, err := m.Profile.ManagerName(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			if name == "" {
				name = domain.DefaultManagerName
			}
			inbound, _ := m.Profile.InboundCallsEnabled(ctx, c.UserID)
			var b strings.Builder
			fmt.Fprintf(&b, "Assistant name: %s\nInbound calls enabled: %t\n", name, inbound)
			if m.Aliases != nil {
				al, err := m.Aliases.List(ctx, c.UserID)
				if err != nil {
					return Output{}, err
				}
				keys := make([]string, 0, len(al))
				for k := range al {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				b.WriteString("Aliases:")
				for _, k := range keys {
					fmt.Fprintf(&b, " %s=%s;", k, al[k])
				}
				b.WriteString("\n")
			}
			if m.Policy != nil {
				if v, err := m.Policy.Get(ctx, c.UserID); err == nil {
					fmt.Fprintf(&b, "Permission policy (version %d): %s\n", v.Version, jsonLine(v.Policy, 1500))
				}
			}
			return Output{Text: b.String()}, nil
		},
	}
}

func setManagerName(m MoreDeps) *Tool {
	return &Tool{
		Name:        "set_manager_name",
		Description: "Rename the assistant (the name the owner calls it). Giving the default name puts it back.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"name": str("the new name")}, "name"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Name string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			name, err := domain.NormalizeManagerName(a.Name)
			if err != nil {
				return Output{}, err
			}
			prior, _ := m.Profile.ManagerName(ctx, c.UserID)
			if prior == "" {
				prior = domain.DefaultManagerName
			}
			if err := m.Profile.SetManagerName(ctx, c.UserID, name); err != nil {
				return Output{}, err
			}
			return Output{Text: "The assistant is now called " + name + ".", Summary: "Renamed the assistant to " + name, UndoData: rawJSON(map[string]string{"name": prior})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ Name string }
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if err := m.Profile.SetManagerName(ctx, user, u.Name); err != nil {
				return "", err
			}
			return "The name is back to " + u.Name + ".", nil
		},
	}
}

func setInboundCalls(m MoreDeps) *Tool {
	return &Tool{
		Name:        "set_inbound_calls",
		Description: "Turn the assistant answering inbound calls on or off.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"enabled": flag("true to answer inbound calls")}, "enabled"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Enabled *bool }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			if a.Enabled == nil {
				return Output{}, errors.New("enabled is required")
			}
			prior, _ := m.Profile.InboundCallsEnabled(ctx, c.UserID)
			if err := m.Profile.SetInboundCallsEnabled(ctx, c.UserID, *a.Enabled); err != nil {
				return Output{}, err
			}
			s := fmt.Sprintf("Inbound calls enabled set to %t", *a.Enabled)
			return Output{Text: s + ".", Summary: s, UndoData: rawJSON(map[string]bool{"enabled": prior})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ Enabled bool }
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if err := m.Profile.SetInboundCallsEnabled(ctx, user, u.Enabled); err != nil {
				return "", err
			}
			return fmt.Sprintf("Inbound calls enabled is back to %t.", u.Enabled), nil
		},
	}
}

func setAlias(m MoreDeps) *Tool {
	return &Tool{
		Name:        "set_alias",
		Description: "Add or replace an alias so a nickname means a name ('mummy' means 'mom').",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"alias": str("the nickname"), "target": str("what it means")}, "alias", "target"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Alias, Target string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			alias, err := domain.NormalizeAlias(a.Alias, 40)
			if err != nil {
				return Output{}, err
			}
			target, err := domain.NormalizeAlias(a.Target, 80)
			if err != nil {
				return Output{}, err
			}
			if alias == target {
				return Output{}, errors.New("an alias cannot mean itself")
			}
			all, err := m.Aliases.List(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			prior, had := all[alias]
			if err := m.Aliases.Put(ctx, c.UserID, alias, target); err != nil {
				return Output{}, err
			}
			return Output{Text: alias + " now means " + target + ".", Summary: "Set the alias " + alias + " to mean " + target,
				UndoData: rawJSON(map[string]any{"alias": alias, "prior": prior, "had": had})}, nil
		},
		Undo: undoAlias(m),
	}
}

func undoAlias(m MoreDeps) func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
	return func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
		var u struct {
			Alias, Prior string
			Had          bool
		}
		if err := undoInto(data, &u); err != nil {
			return "", err
		}
		if u.Had {
			if err := m.Aliases.Put(ctx, user, u.Alias, u.Prior); err != nil {
				return "", err
			}
			return "The alias " + u.Alias + " is back to " + u.Prior + ".", nil
		}
		if _, err := m.Aliases.Delete(ctx, user, u.Alias); err != nil {
			return "", err
		}
		return "The alias " + u.Alias + " was removed.", nil
	}
}

func removeAlias(m MoreDeps) *Tool {
	return &Tool{
		Name:        "remove_alias",
		Description: "Remove an alias.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"alias": str("the nickname to remove")}, "alias"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Alias string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			alias, err := domain.NormalizeAlias(a.Alias, 40)
			if err != nil {
				return Output{}, err
			}
			all, err := m.Aliases.List(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			prior, had := all[alias]
			if !had {
				return Output{Text: "There is no alias " + alias + ". Nothing was removed."}, nil
			}
			if _, err := m.Aliases.Delete(ctx, c.UserID, alias); err != nil {
				return Output{}, err
			}
			return Output{Text: "Removed the alias " + alias + ".", Summary: "Removed the alias " + alias, UndoData: rawJSON(map[string]any{"alias": alias, "prior": prior, "had": true})}, nil
		},
		Undo: undoAlias(m),
	}
}

func listNotifications(m MoreDeps) *Tool {
	return &Tool{
		Name:        "list_notifications",
		Description: "Show the owner's most recent notifications (newest last). It only reads: it does not mark anything as shown.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"limit": num("how many (default 15, max 50)")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Limit int }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			n := clampLimit(a.Limit, 15, 50)
			latest, err := m.Notifications.LatestSeq(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			after := latest - int64(n)
			if after < 0 {
				after = 0
			}
			rows, err := m.Notifications.After(ctx, c.UserID, after, n)
			if err != nil {
				return Output{}, err
			}
			if len(rows) == 0 {
				return Output{Text: "No notifications."}, nil
			}
			var b strings.Builder
			for _, r := range rows {
				fmt.Fprintf(&b, "%s | %s | %s | %s\n", r.CreatedAt.In(ist).Format("2 Jan 3:04pm"), r.Kind, truncate(r.Title, 80), truncate(r.Body, 160))
			}
			return Output{Text: fmt.Sprintf("%d notifications (when | kind | title | body):\n%s", len(rows), b.String())}, nil
		},
	}
}

// ---- research ----

func listResearch(m MoreDeps) *Tool {
	return &Tool{
		Name:        "list_research",
		Description: "List the owner's recent research runs with ids, status and objective.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"limit": num("how many (default 15, max 50)")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Limit int }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			runs, err := m.Research.ListRuns(ctx, c.UserID, clampLimit(a.Limit, 15, 50))
			if err != nil {
				return Output{}, err
			}
			if len(runs) == 0 {
				return Output{Text: "There are no research runs."}, nil
			}
			var b strings.Builder
			var ids []string
			for _, r := range runs {
				ids = append(ids, r.ID.String())
				fmt.Fprintf(&b, "%s | %s | %s | %s\n", r.ID, r.Status, r.Playbook, truncate(r.Objective, 100))
			}
			return Output{Text: fmt.Sprintf("%d runs (id | status | playbook | objective):\n%s", len(runs), b.String()), IDs: ids}, nil
		},
	}
}

func showResearch(m MoreDeps) *Tool {
	return &Tool{
		Name:        "show_research",
		Description: "Show one research run: its status, progress, findings and the first pieces of evidence behind them.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"id": str("run id from list_research")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			run, err := m.Research.GetRun(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			ev, err := m.Research.ListEvidenceForRun(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Run %s | %s | %s\nObjective: %s\nProgress: %v\nFindings: %d\n%s\nEvidence: %d items\n", run.ID, run.Status, run.Playbook, run.Objective, run.ProgressMetrics, len(run.Findings), jsonLine(run.Findings, 2500), len(ev))
			for i, e := range ev {
				if i == 10 {
					break
				}
				b.WriteString(jsonLine(e, 300) + "\n")
			}
			return Output{Text: b.String(), Untrusted: true}, nil
		},
	}
}

func startResearch(d Deps) *Tool {
	m := d.More
	return &Tool{
		Name:        "start_research",
		Description: "Start a research run on an objective. It runs in the background as a task; check it with list_research and show_research.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"objective": str("what to find out"), "depth": str("optional: quick, standard or deep")}, "objective"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				Objective string
				Depth     domain.ResearchDepth
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			req := domain.ResearchRequest{Objective: a.Objective, Depth: a.Depth}
			if err := req.Normalize(); err != nil {
				return Output{}, err
			}
			run, err := m.Research.StartRun(ctx, c.UserID, req)
			if err != nil {
				return Output{}, err
			}
			if err := scheduleResearch(ctx, d, run); err != nil {
				_ = m.Research.FailRun(ctx, c.UserID, run.ID, "could not schedule the run: "+err.Error())
				return Output{}, err
			}
			return Output{Text: fmt.Sprintf("Research started (run %s).", run.ID), Summary: "Started research: " + truncate(run.Objective, 100), IDs: []string{run.ID.String()}, Wake: true,
				UndoData: rawJSON(idPayload{run.ID})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u idPayload
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			if err := m.Research.CancelRun(ctx, user, u.ID); err != nil {
				return "", err
			}
			return "The research run was cancelled.", nil
		},
	}
}

func scheduleResearch(ctx context.Context, d Deps, run *domain.ResearchRun) error {
	_, err := d.Tasks.CreateTask(ctx, domain.NewResearchRunTask(run.UserID, run.ID, run.NextStepIndex, run.Revision, run.Objective), nil)
	if err != nil && !errors.Is(err, domain.ErrIdempotentConflict) {
		return err
	}
	return nil
}

func controlResearch(d Deps) *Tool {
	m := d.More
	return &Tool{
		Name:        "control_research",
		Description: "Pause, resume or cancel a research run (action pause, resume or cancel). Cancel cannot be undone.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"id": str("run id from list_research"), "action": str("pause, resume or cancel")}, "id", "action"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID, Action string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			out := Output{}
			switch a.Action {
			case "pause":
				if err := m.Research.PauseRun(ctx, c.UserID, id); err != nil {
					return Output{}, err
				}
				out.UndoData = rawJSON(map[string]string{"id": id.String(), "action": "resume"})
			case "resume":
				run, err := m.Research.ResumeRun(ctx, c.UserID, id)
				if err != nil {
					return Output{}, err
				}
				if !run.Status.IsTerminal() && run.Status != domain.PhasePaused {
					if d.Tasks != nil {
						if err := scheduleResearch(ctx, d, run); err != nil {
							return Output{}, err
						}
					}
				}
				out.UndoData = rawJSON(map[string]string{"id": id.String(), "action": "pause"})
			case "cancel":
				if err := m.Research.CancelRun(ctx, c.UserID, id); err != nil {
					return Output{}, err
				}
			default:
				return Output{}, errors.New("action must be pause, resume or cancel")
			}
			out.Text = "Research run " + a.Action + ": done."
			out.Summary = "Research run " + id.String() + ": " + a.Action
			return out, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ ID, Action string }
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			id, err := uuid.Parse(u.ID)
			if err != nil {
				return "", ErrNoUndo
			}
			if u.Action == "pause" {
				return "The run is paused again.", m.Research.PauseRun(ctx, user, id)
			}
			_, err = m.Research.ResumeRun(ctx, user, id)
			return "The run is resumed.", err
		},
	}
}

// ---- packages, companies ----

func showPackage(m MoreDeps) *Tool {
	return &Tool{
		Name:        "show_package",
		Description: "Show a prepared application package (resume choice, projects, cover letter, answers, missing information). Give listing_id (from list_listings) or package_id (from the approval). Approve or reject it with decide_approval.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"listing_id": str("listing id from list_listings"), "package_id": str("package id")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				ListingID string `json:"listing_id"`
				PackageID string `json:"package_id"`
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			var pkg *domain.ApplicationPackage
			var err error
			switch {
			case a.ListingID != "":
				id, perr := c.ParseSeenID(a.ListingID)
				if perr != nil {
					return Output{}, perr
				}
				pkg, err = m.Packages.PackageFor(ctx, c.UserID, id)
			case a.PackageID != "":
				id, perr := uuid.Parse(strings.TrimSpace(a.PackageID))
				if perr != nil {
					return Output{}, errors.New("package_id is not a valid id")
				}
				pkg, err = m.Packages.GetPackage(ctx, c.UserID, id)
			default:
				return Output{}, errors.New("give listing_id or package_id")
			}
			if err != nil {
				return Output{}, err
			}
			if pkg == nil {
				return Output{Text: "No package has been prepared for that listing."}, nil
			}
			return Output{Text: "Package " + pkg.ID.String() + ":\n" + jsonLine(pkg, 6000), IDs: []string{pkg.ID.String()}}, nil
		},
	}
}

func listCompanies(m MoreDeps) *Tool {
	return &Tool{
		Name:        "list_companies",
		Description: "List the companies the opportunity scan found, with ids and their open-listing counts.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"limit": num("how many (default 30, max 100)")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Limit int }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			list, err := m.Companies.ListCompanySummaries(ctx, c.UserID, clampLimit(a.Limit, 30, 100))
			if err != nil {
				return Output{}, err
			}
			if len(list) == 0 {
				return Output{Text: "No companies yet."}, nil
			}
			var b strings.Builder
			var ids []string
			for _, s := range list {
				ids = append(ids, s.ID.String())
				fmt.Fprintf(&b, "%s | %s | %s | open listings %d\n", s.ID, truncate(s.Name, 60), s.Status, s.OpenListings)
			}
			return Output{Text: fmt.Sprintf("%d companies (id | name | status | open):\n%s", len(list), b.String()), IDs: ids}, nil
		},
	}
}

func showCompany(m MoreDeps) *Tool {
	return &Tool{
		Name:        "show_company",
		Description: "Show one company with its listings, researched facts (pay, size and so on) and the evidence behind them.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"id": str("company id from list_companies")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			d, err := m.Companies.GetCompanyDetail(ctx, c.UserID, id)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: jsonLine(d, 5000), Untrusted: true}, nil
		},
	}
}

// ---- tasks ----

func ownedTask(ctx context.Context, d Deps, c *Call, rawID string) (uuid.UUID, *domain.Task, error) {
	id, err := c.ParseSeenID(rawID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	byID, _, err := ownedTasks(ctx, d, c.UserID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	t := byID[id]
	if t == nil {
		return uuid.Nil, nil, errors.New("that task was not found")
	}
	return id, t, nil
}

func taskHistory(d Deps) *Tool {
	m := d.More
	return &Tool{
		Name:        "task_history",
		Description: "Show what happened inside one task: its event log, each attempt (and why it failed), and the audit trail with whether the chain is intact. Use it to answer why something failed.",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"id": str("task id from list_tasks")}, "id"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, t, err := ownedTask(ctx, d, c, a.ID)
			if err != nil {
				return Output{}, err
			}
			ev, err := m.TaskInfo.GetEvents(ctx, id)
			if err != nil {
				return Output{}, err
			}
			at, err := m.TaskInfo.GetAttempts(ctx, id)
			if err != nil {
				return Output{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Task %s | %s | %s\nEvents (%d, last 20):\n", id, t.Status, t.Title, len(ev))
			for i := max(0, len(ev)-20); i < len(ev); i++ {
				b.WriteString(jsonLine(ev[i], 250) + "\n")
			}
			fmt.Fprintf(&b, "Attempts (%d):\n", len(at))
			for _, x := range at {
				b.WriteString(jsonLine(x, 300) + "\n")
			}
			if au, err := m.TaskInfo.Audit(ctx, c.UserID, id); err == nil {
				fmt.Fprintf(&b, "Audit entries: %d, chain intact: %t\n", len(au), domain.VerifyAudit(au) == -1)
			}
			return Output{Text: b.String(), Untrusted: true}, nil
		},
	}
}

func scorecard(m MoreDeps) *Tool {
	return &Tool{
		Name:        "scorecard",
		Description: "Show the scorecard (counts and rates of what Griffin did) for the last N days (default 30, max 365).",
		Effect:      EffectRead,
		Schema:      obj(map[string]any{"days": num("days back")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Days int }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			days := clampLimit(a.Days, 30, 365)
			rows, err := m.TaskInfo.ScoreRows(ctx, c.UserID, c.Now.AddDate(0, 0, -days))
			if err != nil {
				return Output{}, err
			}
			return Output{Text: domain.ScorecardMarkdown(domain.ComputeScorecard(rows), days)}, nil
		},
	}
}

func archiveTasks(d Deps) *Tool {
	m := d.More
	return &Tool{
		Name:        "archive_tasks",
		Description: "Archive finished tasks (hide them from the Tasks page; they can be brought back). Give task ids from list_tasks. A task still running is skipped.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"ids": strs("task ids from list_tasks")}, "ids"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ IDs []string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			if len(a.IDs) == 0 {
				return Output{}, errors.New("give at least one task id")
			}
			var ids []uuid.UUID
			for _, s := range a.IDs {
				id, t, err := ownedTask(ctx, d, c, s)
				if err != nil {
					return Output{}, err
				}
				if t.ParentID != nil {
					return Output{}, errors.New("a subtask is archived with its parent task")
				}
				ids = append(ids, id)
			}
			n, err := m.Archive.ArchiveTasks(ctx, c.UserID, ids, c.Now)
			if err != nil {
				return Output{}, err
			}
			if n == 0 {
				return Output{Text: "Nothing was archived: only finished tasks can be."}, nil
			}
			return Output{Text: fmt.Sprintf("Archived %d task(s).", n), Summary: fmt.Sprintf("Archived %d task(s)", n), UndoData: rawJSON(map[string]any{"ids": ids})}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ IDs []uuid.UUID }
			if err := undoInto(data, &u); err != nil {
				return "", err
			}
			var n int64
			for _, id := range u.IDs {
				k, err := m.Archive.UnarchiveTask(ctx, user, id)
				if err != nil {
					return "", err
				}
				n += k
			}
			return fmt.Sprintf("Unarchived %d task(s).", n), nil
		},
	}
}

type deleteTasksPayload struct {
	IDs      []uuid.UUID `json:"ids,omitempty"`
	Archived bool        `json:"archived,omitempty"`
}

func deleteTasks(d Deps) *Tool {
	m := d.More
	return &Tool{
		Name: "delete_tasks",
		Description: "Permanently delete tasks (cannot be undone): either task ids from list_tasks, or archived=true to delete every archived task. To only stop a task use cancel_tasks; to hide it use archive_tasks. " +
			"When a routine started this, it waits for the owner's approval.",
		Effect: EffectExternal,
		Schema: obj(map[string]any{"ids": strs("task ids from list_tasks"), "archived": flag("true to delete all archived tasks instead")}),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a struct {
				IDs      []string
				Archived bool
			}
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			p := deleteTasksPayload{Archived: a.Archived}
			if a.Archived == (len(a.IDs) > 0) {
				return nil, "", errors.New("give either ids or archived=true")
			}
			for _, s := range a.IDs {
				id, err := uuid.Parse(strings.TrimSpace(s))
				if err != nil {
					return nil, "", fmt.Errorf("id %q is not a valid id", s)
				}
				p.IDs = append(p.IDs, id)
			}
			summary := "Permanently delete every archived task"
			if !p.Archived {
				summary = fmt.Sprintf("Permanently delete %d task(s)", len(p.IDs))
			}
			return rawJSON(p), summary, nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			var p deleteTasksPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				return "", err
			}
			if p.Archived {
				if m.Archive == nil {
					return "", errors.New("archiving is not available")
				}
				n, err := m.Archive.DeleteArchived(ctx, user)
				return fmt.Sprintf("Deleted %d archived task(s).", n), err
			}
			byID, _, err := ownedTasks(ctx, d, user)
			if err != nil {
				return "", err
			}
			n := 0
			for _, id := range p.IDs {
				if byID[id] == nil { // only the owner's own tasks
					continue
				}
				if err := m.DeleteTask(ctx, id); err != nil {
					return "", err
				}
				n++
			}
			return fmt.Sprintf("Deleted %d task(s).", n), nil
		},
	}
}

// ---- DSA, brief, approvals, read-only pages ----

func dsaToday(m MoreDeps) *Tool {
	return &Tool{
		Name:        "dsa_today",
		Description: "Show the DSA coach's problems for today (slug | title | difficulty | state). The slug is the id for mark_dsa_solved.",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			items, err := m.DSA.Today(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			var b strings.Builder
			var ids []string
			for _, it := range items {
				if it.State == domain.DsaSolved || len(ids) == 15 {
					continue
				}
				ids = append(ids, it.Slug)
				fmt.Fprintf(&b, "%s | %s | %s | %s\n", it.Slug, truncate(it.Title, 60), it.Difficulty, it.State)
			}
			if len(ids) == 0 {
				return Output{Text: "No open DSA problems."}, nil
			}
			return Output{Text: fmt.Sprintf("%d open problems:\n%s", len(ids), b.String()), IDs: ids}, nil
		},
	}
}

func markDsaSolved(m MoreDeps) *Tool {
	return &Tool{
		Name:        "mark_dsa_solved",
		Description: "Mark one DSA problem solved by hand (slug from dsa_today).",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"slug": str("problem slug from dsa_today")}, "slug"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Slug string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			slug := strings.TrimSpace(a.Slug)
			if err := c.RequireSeen(slug); err != nil {
				return Output{}, err
			}
			if err := m.DSA.MarkSolved(ctx, c.UserID, slug); err != nil {
				return Output{}, err
			}
			return Output{Text: slug + " is marked solved.", Summary: "Marked the DSA problem " + slug + " solved"}, nil
		},
	}
}

func syncDsa(m MoreDeps) *Tool {
	return &Tool{
		Name:        "sync_dsa",
		Description: "Check LeetCode now and mark the problems the owner has just solved.",
		Effect:      EffectInternal,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			if _, err := m.DSA.Today(ctx, c.UserID); err != nil {
				return Output{}, err
			}
			solved, err := m.DSA.Sync(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			s := fmt.Sprintf("LeetCode sync marked %d problem(s) solved", len(solved))
			return Output{Text: s + ": " + strings.Join(solved, ", "), Summary: s}, nil
		},
	}
}

func dailyBrief(m MoreDeps) *Tool {
	return &Tool{
		Name:        "daily_brief",
		Description: "Build the owner's brief for today (plan, deadlines, what needs attention).",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			b, err := m.Brief.Build(ctx, c.UserID)
			if err != nil {
				return Output{}, err
			}
			if b.Empty {
				return Output{Text: "Nothing needs attention today."}, nil
			}
			return Output{Text: b.Text}, nil
		},
	}
}

func editApproval(m MoreDeps) *Tool {
	return &Tool{
		Name:        "edit_approval",
		Description: "Change the text of a pending approval (for example a draft message) before the owner decides. Id from list_approvals. It does not approve anything.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"id": str("approval id from list_approvals"), "text": str("the new text")}, "id", "text"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ ID, Text string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			id, err := c.ParseSeenID(a.ID)
			if err != nil {
				return Output{}, err
			}
			if strings.TrimSpace(a.Text) == "" {
				return Output{}, errors.New("text is required")
			}
			ap, err := m.ApprovalEdit.EditApproval(ctx, id, c.UserID, a.Text)
			if err != nil {
				return Output{}, err
			}
			return Output{Text: "Edited the approval " + ap.ID.String() + ".", Summary: "Edited the text of approval " + ap.ID.String()}, nil
		},
	}
}

func rowsTool(name, desc string, schema map[string]any, fetch func(ctx context.Context, user uuid.UUID, raw json.RawMessage) (string, []string, error)) *Tool {
	return &Tool{Name: name, Description: desc, Effect: EffectRead, Schema: schema,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			text, ids, err := fetch(ctx, c.UserID, raw)
			if err != nil {
				return Output{}, err
			}
			if text == "" {
				return Output{Text: "Nothing to show."}, nil
			}
			return Output{Text: text, IDs: ids, Untrusted: true}, nil
		}}
}

func rowLines[T any](rows []T, id func(T) string) (string, []string) {
	var b strings.Builder
	var ids []string
	for i, r := range rows {
		if i == 40 {
			break
		}
		if id != nil {
			ids = append(ids, id(r))
		}
		b.WriteString(jsonLine(r, 350) + "\n")
	}
	return b.String(), ids
}

func listAssessments(m MoreDeps) *Tool {
	return rowsTool("list_assessments", "List the pending technical assessments found in the owner's email (company, platform, link, deadline).", nil,
		func(ctx context.Context, u uuid.UUID, _ json.RawMessage) (string, []string, error) {
			rows, err := m.Assessments(ctx, u)
			if err != nil {
				return "", nil, err
			}
			t, ids := rowLines(rows, func(r *domain.AssessmentRecord) string { return r.ID.String() })
			return t, ids, nil
		})
}

func listDiscoveredJobs(m MoreDeps) *Tool {
	return rowsTool("list_discovered_jobs", "List jobs the discovery sweep found on company job boards. Optional status filter.", obj(map[string]any{"status": str("optional status")}),
		func(ctx context.Context, u uuid.UUID, raw json.RawMessage) (string, []string, error) {
			var a struct{ Status string }
			if err := decode(raw, &a); err != nil {
				return "", nil, err
			}
			rows, err := m.DiscoveredJobs(ctx, u, a.Status, 40)
			if err != nil {
				return "", nil, err
			}
			t, ids := rowLines(rows, func(r *domain.DiscoveredJob) string { return r.ID.String() })
			return t, ids, nil
		})
}

func listInboundEvents(m MoreDeps) *Tool {
	return rowsTool("list_inbound_events", "List the phone notifications and events Griffin received (messages, mails), newest first. actionable=true shows only those that need the owner.",
		obj(map[string]any{"actionable": flag("only events that need action"), "limit": num("how many (default 20, max 50)")}),
		func(ctx context.Context, u uuid.UUID, raw json.RawMessage) (string, []string, error) {
			var a struct {
				Actionable bool
				Limit      int
			}
			if err := decode(raw, &a); err != nil {
				return "", nil, err
			}
			rows, err := m.InboundEvents(ctx, u, clampLimit(a.Limit, 20, 50), a.Actionable)
			if err != nil {
				return "", nil, err
			}
			t, ids := rowLines(rows, func(r *domain.InboundEvent) string { return r.ID.String() })
			return t, ids, nil
		})
}

func sourcesHealth(m MoreDeps) *Tool {
	return rowsTool("sources_health", "Show how each opportunity source is doing (last scan, last error), so a quiet source is not mistaken for no openings.", nil,
		func(ctx context.Context, u uuid.UUID, _ json.RawMessage) (string, []string, error) {
			rows, err := m.SourceHealth(ctx, u)
			if err != nil {
				return "", nil, err
			}
			t, _ := rowLines(rows, nil)
			return t, nil, nil
		})
}
