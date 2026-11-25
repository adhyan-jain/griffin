package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ApplicationPrepTitle is the routine that prepares application packages for the best eligible listings, in code.
const ApplicationPrepTitle = "APPLICATION PREP"

// ApplicationPackageTaskPrefix starts the title of the task an application package is held on while the owner
// decides: the approvals table needs a task, and this one only carries the approval. It does nothing.
const ApplicationPackageTaskPrefix = "APPLICATION PACKAGE - "

// PackageApprovalAction is the approval type of an application package. Approving it accepts the package as
// written; it does not send anything anywhere.
const PackageApprovalAction = "application_package"

// Package statuses.
const (
	PackageDraft            = "draft"              // stored; the approval has not been requested yet
	PackageNeedsOwner       = "needs_owner"        // information only the owner can give is missing
	PackageReadyForApproval = "ready_for_approval" // complete, and waiting on the approvals page
	PackageApproved         = "approved"
	PackageRejected         = "rejected"
)

// PackageStatuses is every status the schema allows.
var PackageStatuses = map[string]bool{
	PackageDraft: true, PackageNeedsOwner: true, PackageReadyForApproval: true, PackageApproved: true, PackageRejected: true,
}

// PackageProject is one of the owner's projects chosen for a posting: the owner's own words for it (verbatim profile
// facts) and the words of the posting that made it relevant.
type PackageProject struct {
	Project string        `json:"project"`
	Facts   []PackageFact `json:"facts"`
	Skills  []string      `json:"skills"`
	// Quote is the exact words of the posting that match the project's skills.
	Quote string `json:"quote"`
}

// PackageFact is one verbatim profile fact.
type PackageFact struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text"`
}

// PackageAnswer is the owner's answer to a question the posting asks, with where it came from: a profile field or
// fact. An answer exists only when the profile has it; an unknown one is listed under missing information instead.
type PackageAnswer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	// Source says which profile field or fact the answer is from, or "written from your profile facts" for a letter
	// paragraph that was fact-checked.
	Source string `json:"source"`
	// Quote is the words of the posting that asked the question.
	Quote string `json:"quote,omitempty"`
}

// RequiredDocument is a document the posting asks for and whether the owner has it ready.
type RequiredDocument struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ready", "written" or "missing"
	Note   string `json:"note,omitempty"`
	Quote  string `json:"quote,omitempty"`
}

// ApplicationPackage is everything prepared for one application, for the owner to approve.
type ApplicationPackage struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"-"`
	ListingID  uuid.UUID `json:"listing_id"`
	ResumeFile string    `json:"resume_file"`
	RoleClass  string    `json:"role_class"`

	SelectedProjects   []PackageProject   `json:"selected_projects"`
	CoverLetter        string             `json:"cover_letter"`
	Answers            []PackageAnswer    `json:"answers"`
	RequiredDocuments  []RequiredDocument `json:"required_documents"`
	Deadline           *time.Time         `json:"deadline"`
	MissingInformation []string           `json:"missing_information"`
	// Evidence is the words of the posting each choice rests on (a project's match, a question, a required document).
	Evidence []Reason `json:"evidence"`

	Status     string     `json:"status"`
	ApprovalID *uuid.UUID `json:"approval_id"`
	TaskID     *uuid.UUID `json:"task_id,omitempty"`

	// DescriptionHash and ProfileHash say what the package was made from: a changed posting or profile makes a
	// package that still needs the owner stale, so the routine prepares it again.
	DescriptionHash *string `json:"-"`
	ProfileHash     string  `json:"-"`
	ModelUsed       *string `json:"model_used,omitempty"`
	ModelCalls      int     `json:"model_calls"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PackageBrief is what the Internships list shows of a listing's package.
type PackageBrief struct {
	ID         uuid.UUID  `json:"id"`
	Status     string     `json:"status"`
	ApprovalID *uuid.UUID `json:"approval_id"`
	Missing    int        `json:"missing_count"`

	// What the routine needs to tell a package that is still current from a stale one; not sent to the web.
	ListingID       uuid.UUID `json:"-"`
	DescriptionHash *string   `json:"-"`
	ProfileHash     string    `json:"-"`
}

// IsPackageTask reports whether a task only holds an application package for approval.
func IsPackageTask(title string) bool {
	return strings.HasPrefix(strings.TrimSpace(title), ApplicationPackageTaskPrefix)
}

// IsApplicationPrep reports whether a task is the APPLICATION PREP routine.
func IsApplicationPrep(title string) bool {
	return strings.Contains(strings.ToUpper(title), ApplicationPrepTitle)
}

// ResumeKeyFor names which of the owner's resumes a role class gets: AI and research roles the AI/ML one, backend and
// infrastructure roles the backend one, everything else the general software-engineering one.
func ResumeKeyFor(roleClass string) string {
	switch roleClass {
	case RoleAIML, RoleResearch:
		return "AI/ML"
	case RoleBackend, RoleInfra:
		return "backend"
	}
	return "software engineering"
}

// ResumeFor is the resume file for a role class.
func ResumeFor(roleClass string) string { return ResumeFiles[ResumeKeyFor(roleClass)] }

// Text is what a submission would carry: the cover letter and the answers, word for word as the owner approved them.
func (p *ApplicationPackage) Text() string {
	var b strings.Builder
	if strings.TrimSpace(p.CoverLetter) != "" {
		b.WriteString("Cover letter:\n" + strings.TrimSpace(p.CoverLetter) + "\n\n")
	}
	if len(p.Answers) > 0 {
		b.WriteString("Answers:\n")
		for _, a := range p.Answers {
			b.WriteString("Q: " + a.Question + "\nA: " + a.Answer + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// PackageApprovalSummary is the one line the approvals page shows for a package.
func PackageApprovalSummary(l *Listing) string {
	return "Review the application package for " + strings.TrimSpace(l.Role+" at "+l.Company)
}

// PackageApprovalPayload is what the approval of a package carries: the package's id, which is all the decision
// needs, and enough to tell what it is about and what approving it would later hand on (the posting, the resume).
// Nothing in it is a message text, so nothing treats it as one.
func PackageApprovalPayload(l *Listing, p *ApplicationPackage) json.RawMessage {
	projects := make([]string, 0, len(p.SelectedProjects))
	for _, pr := range p.SelectedProjects {
		projects = append(projects, pr.Project)
	}
	payload, _ := json.Marshal(map[string]any{
		"package_id": p.ID, "listing_id": l.ID, "company": l.Company, "role": l.Role, "portal_url": l.URL,
		"resume_path": p.ResumeFile, "projects": projects, "deadline": p.Deadline,
	})
	return payload
}

// PackageIDOf reads the package an approval is about.
func PackageIDOf(a *Approval) (uuid.UUID, bool) {
	var p struct {
		PackageID uuid.UUID `json:"package_id"`
	}
	if a == nil || json.Unmarshal(a.ActionPayload, &p) != nil || p.PackageID == uuid.Nil {
		return uuid.Nil, false
	}
	return p.PackageID, true
}
