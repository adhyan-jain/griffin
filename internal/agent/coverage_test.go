package agent

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// routeTools maps every owner-facing route (one behind userAuth in internal/api/server.go) to the tool that does
// the same thing for the owner in chat. Add a route to the server and this test fails until it is mapped here, or
// excluded below with a reason.
var routeTools = map[string]string{
	"POST /api/v1/tasks":                              "start_job",
	"GET /api/v1/tasks":                               "list_tasks",
	"GET /api/v1/tasks/{id}":                          "list_tasks",
	"POST /api/v1/tasks/{id}/cancel":                  "cancel_tasks",
	"POST /api/v1/tasks/{id}/resume":                  "retry_task",
	"DELETE /api/v1/tasks/{id}":                       "delete_tasks",
	"POST /api/v1/tasks/{id}/archive":                 "archive_tasks",
	"POST /api/v1/tasks/{id}/unarchive":               "archive_tasks", // undo of archive_tasks
	"POST /api/v1/tasks/archived/delete":              "delete_tasks",
	"GET /api/v1/tasks/{id}/events":                   "task_history",
	"GET /api/v1/tasks/{id}/attempts":                 "task_history",
	"GET /api/v1/tasks/{id}/audit":                    "task_history",
	"GET /api/v1/scorecard":                           "scorecard",
	"POST /api/v1/calls/outbound":                     "place_call",
	"GET /api/v1/brief":                               "daily_brief",
	"GET /api/v1/events/inbound":                      "list_inbound_events",
	"GET /api/v1/dsa":                                 "dsa_today",
	"POST /api/v1/dsa/sync":                           "sync_dsa",
	"POST /api/v1/dsa/{slug}/solved":                  "mark_dsa_solved",
	"GET /api/v1/approvals":                           "list_approvals",
	"POST /api/v1/approvals/{id}/respond":             "decide_approval",
	"POST /api/v1/approvals/batch":                    "decide_approval",
	"PATCH /api/v1/approvals/{id}":                    "edit_approval",
	"GET /api/v1/artifacts":                           "list_library",
	"GET /api/v1/artifacts/{id}":                      "read_artifact_text",
	"DELETE /api/v1/artifacts/{id}":                   "delete_artifact",
	"PUT /api/v1/artifacts/{id}/text":                 "update_artifact_text",
	"POST /api/v1/lists":                              "create_list",
	"GET /api/v1/settings/manager-name":               "show_settings",
	"PUT /api/v1/settings/manager-name":               "set_manager_name",
	"GET /api/v1/settings/calls":                      "show_settings",
	"PUT /api/v1/settings/calls":                      "set_inbound_calls",
	"GET /api/v1/vault":                               "list_vault",
	"GET /api/v1/inbox/assessments":                   "list_assessments",
	"GET /api/v1/discovery/jobs":                      "list_discovered_jobs",
	"GET /api/v1/hosts":                               "list_devices",
	"PATCH /api/v1/hosts/{id}":                        "update_device",
	"DELETE /api/v1/hosts/{id}":                       "revoke_device",
	"GET /api/v1/listings":                            "list_listings",
	"POST /api/v1/listings/{id}/close":                "close_listings",
	"POST /api/v1/listings/{id}/prepare":              "prepare_applications",
	"GET /api/v1/listings/{id}/package":               "show_package",
	"GET /api/v1/packages/{id}":                       "show_package",
	"GET /api/v1/sources/health":                      "sources_health",
	"GET /api/v1/notifications":                       "list_notifications",
	"GET /api/v1/aliases":                             "show_settings",
	"PUT /api/v1/aliases":                             "set_alias",
	"DELETE /api/v1/aliases/{alias}":                  "remove_alias",
	"GET /api/v1/permissions":                         "show_settings",
	"POST /api/v1/outreach/campaigns":                 "create_campaign",
	"GET /api/v1/outreach/campaigns":                  "list_campaigns",
	"PATCH /api/v1/outreach/campaigns/{id}":           "update_campaign",
	"GET /api/v1/outreach/campaigns/{id}/targets":     "list_campaign_targets",
	"POST /api/v1/outreach/campaigns/{id}/targets":    "add_campaign_targets",
	"POST /api/v1/outreach/campaigns/{id}/draft":      "draft_campaign_messages",
	"POST /api/v1/outreach/campaigns/{id}/next":       "next_outreach_target",
	"PATCH /api/v1/outreach/targets/{id}":             "update_outreach_target",
	"POST /api/v1/outreach/replies":                   "record_outreach_reply",
	"GET /api/v1/outreach/awaiting":                   "awaiting_replies",
	"POST /api/v1/research":                           "start_research",
	"GET /api/v1/research":                            "list_research",
	"GET /api/v1/research/{id}":                       "show_research",
	"POST /api/v1/research/{id}/pause":                "control_research",
	"POST /api/v1/research/{id}/resume":               "control_research",
	"POST /api/v1/research/{id}/cancel":               "control_research",
	"GET /api/v1/research/{id}/evidence":              "show_research",
	"GET /api/v1/companies":                           "list_companies",
	"GET /api/v1/companies/explorer":                  "list_companies",
	"GET /api/v1/companies/{id}":                      "show_company",
	"GET /api/v1/alumni":                              "list_alumni",
	"GET /api/v1/alumni/explorer":                     "list_alumni",
	"POST /api/v1/alumni/import":                      "import_alumni",
	"PATCH /api/v1/alumni/{id}":                       "update_alumnus",
	"PATCH /api/v1/alumni/{id}/verification":          "verify_alumnus",
	"POST /api/v1/alumni/{id}/outreach":               "file_alumni_outreach",
	"GET /api/v1/alumni/{id}/draft":                   "draft_alumni_message",
	"GET /api/v1/campaigns/vit-alumni/stats":          "vit_campaign_status",
	"GET /api/v1/campaigns/vit-alumni/queue":          "vit_campaign_status",
	"POST /api/v1/campaigns/vit-alumni/build-queue":   "build_vit_queue",
	"PATCH /api/v1/campaigns/vit-alumni/targets/{id}": "update_vit_target",
	"GET /api/v1/alumni/search-query":                 "linkedin_search_queries",
	"POST /api/v1/alumni/search-query":                "linkedin_search_queries",
}

// routeExcluded are owner-facing routes with no chat tool, each with the reason. Keep it short.
var routeExcluded = map[string]string{
	// the chat itself, undo, and its history
	"POST /api/v1/jj/messages":                "the chat endpoint itself",
	"POST /api/v1/jj/undo/{action_id}":        "undo is the chat's own button",
	"GET /api/v1/jj/conversations":            "chat history for the web UI",
	"GET /api/v1/jj/conversations/{id}/turns": "chat history for the web UI",
	"POST /api/v1/tasks/{id}/follow-ups":      "a follow-up question on a task: it is the chat",
	"POST /api/v1/voice/turn":                 "the voice chat endpoint",
	"GET /api/v1/voice/synthesize":            "speech audio stream",
	"GET /api/v1/voice/voices":                "voice picker for the UI",
	"GET /api/v1/tasks/{id}/spoken-summary":   "speech text for the voice UI",
	// raw file streams and uploads
	"POST /api/v1/artifacts":                         "registers a file by storage address (upload plumbing)",
	"POST /api/v1/artifacts/upload":                  "raw file upload",
	"GET /api/v1/artifacts/{id}/download":            "raw file download stream",
	"GET /api/v1/artifacts/{id}/presign":             "makes a download link for the UI",
	"POST /api/v1/vault":                             "raw document upload (arrives already encrypted)",
	"POST /api/v1/calls/record":                      "call audio upload from the phone",
	"GET /api/v1/outreach/campaigns/{id}/export.csv": "CSV download stream (list_campaign_targets shows the same rows)",
	// bookkeeping written by hosts and devices
	"GET /api/v1/vault/misses":         "bookkeeping a host writes about documents it could not find",
	"POST /api/v1/vault/misses":        "bookkeeping a host writes",
	"DELETE /api/v1/vault/misses/{id}": "bookkeeping a host writes",
	"POST /api/v1/events/inbound":      "the phone pushes its notifications here",
	"POST /api/v1/notifications/ack":   "a device's own read cursor; the agent only reads",
	"POST /api/v1/hosts":               "registering a device shows a secret token once: owner UI only",
	// security and preferences the agent must not change itself
	"PUT /api/v1/permissions":            "the permission policy: the agent must not widen its own permissions (it can read it)",
	"PUT /api/v1/dsa/settings":           "a pure preference form on the DSA page",
	"POST /api/v1/approvals/{id}/chat":   "model-assisted rewrite inside the Approvals UI (edit_approval edits the text)",
	"POST /api/v1/approvals/{id}/revise": "model-assisted rewrite inside the Approvals UI (edit_approval edits the text)",
	// background ingestion run by routines
	"POST /api/v1/inbox/sync":    "the phone/laptop posts mail batches; routines run it",
	"POST /api/v1/discovery/run": "a job-board sweep run by the scheduled discovery routine",
	// operator plumbing
	"GET /api/v1/workers/definitions":               "worker definition authoring: operator plumbing",
	"GET /api/v1/workers/definitions/{id}":          "worker definition authoring: operator plumbing",
	"GET /api/v1/workers/definitions/{id}/versions": "worker definition authoring: operator plumbing",
	"POST /api/v1/workers/definitions":              "worker definition authoring: operator plumbing",
	"PUT /api/v1/workers/definitions/{id}":          "worker definition authoring: operator plumbing",
	"POST /api/v1/workers/definitions/{id}/toggle":  "worker definition authoring: operator plumbing",
	"GET /api/v1/agents":                            "legacy agent registry",
	"POST /api/v1/agents":                           "legacy agent registry",
	"POST /api/v1/orchestrator/requests":            "legacy orchestrator",
	"GET /api/v1/orchestrator/requests/{id}":        "legacy orchestrator",
	"GET /api/v1/orchestrator/plans/{id}":           "legacy orchestrator",
	"GET /api/v1/managers":                          "legacy orchestrator",
	"GET /api/v1/managers/{id}":                     "legacy orchestrator",
	"GET /api/v1/runner/status":                     "cloud runner diagnostics for the Settings page",
	"POST /api/v1/runner/wake":                      "cloud runner control: the engine wakes it when work needs it",
	"GET /api/v1/system/status":                     "health probes for the Settings page",
	"GET /api/v1/system/capabilities":               "feature flags for the web UI",
}

var userRouteRe = regexp.MustCompile(`mux\.Handle\("([A-Z]+ [^"]+)",\s*userAuth\(`)

// embedded nil interfaces satisfy a dependency whose methods the coverage test never calls.
type (
	anyVault      struct{ VaultLister }
	anyPeople     struct{ PeopleAPI }
	anyHolder     struct{ AlumniHolder }
	anyImporter   struct{ AlumniImporter }
	anyVIT        struct{ VITAPI }
	anyOutreach   struct{ OutreachAPI }
	anyDrafter    struct{ CampaignDrafter }
	anyLibrary    struct{ LibraryAPI }
	anyHosts      struct{ HostsAPI }
	anyProfile    struct{ ProfileAPI }
	anyAliases    struct{ AliasesAPI }
	anyPolicy     struct{ PolicyReader }
	anyNotes      struct{ NotificationsAPI }
	anyResearch   struct{ ResearchAPI }
	anyPackages   struct{ PackagesAPI }
	anyCompanies  struct{ CompaniesAPI }
	anyTaskInfo   struct{ TaskInfoAPI }
	anyArchive    struct{ ArchiveAPI }
	anyDSA        struct{ DsaAPI }
	anyBrief      struct{ BriefAPI }
	anyApprovalEd struct{ ApprovalEditor }
)

// fullDeps sets every dependency, so the registry holds every tool.
func fullDeps() Deps {
	return Deps{
		Tasks: &fakeTasks{}, Routines: &fakeRoutines{}, Reminders: &fakeReminders{}, Approvals: &fakeApprovals{},
		Search: fakeSearch{}, Fetch: fakeFetch{}, Store: NewMemStore(),
		Listings: &fakeListings{}, Trackers: &fakeTrackers{lists: map[string]string{}}, Vault: anyVault{},
		CheckLink: func(context.Context, string) service.PostingState { return service.PostingMissing },
		Jobs:      true,
		PhoneLive: func(context.Context, uuid.UUID) bool { return true },
		More: MoreDeps{
			People: anyPeople{}, Holder: anyHolder{}, Importer: anyImporter{}, VIT: anyVIT{}, Outreach: anyOutreach{}, Drafter: anyDrafter{},
			Library: anyLibrary{}, Hosts: anyHosts{}, Profile: anyProfile{}, Aliases: anyAliases{}, Policy: anyPolicy{}, Notifications: anyNotes{},
			Research: anyResearch{}, Packages: anyPackages{}, Companies: anyCompanies{}, TaskInfo: anyTaskInfo{}, Archive: anyArchive{},
			DeleteTask: func(context.Context, uuid.UUID) error { return nil }, DSA: anyDSA{}, Brief: anyBrief{}, ApprovalEdit: anyApprovalEd{},
		},
	}
}

func fullRegistry(t *testing.T) *Registry {
	t.Helper()
	d := fullDeps()
	m := &d.More
	m.Assessments = func(context.Context, uuid.UUID) ([]*domain.AssessmentRecord, error) { return nil, nil }
	m.DiscoveredJobs = func(context.Context, uuid.UUID, string, int) ([]*domain.DiscoveredJob, error) { return nil, nil }
	m.InboundEvents = func(context.Context, uuid.UUID, int, bool) ([]*domain.InboundEvent, error) { return nil, nil }
	m.SourceHealth = func(context.Context, uuid.UUID) ([]domain.SourceHealth, error) { return nil, nil }
	reg, err := NewRegistry(NewTools(d)...)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestEveryOwnerRouteHasAToolOrAReason(t *testing.T) {
	src, err := os.ReadFile("../api/server.go")
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	for _, m := range userRouteRe.FindAllStringSubmatch(string(src), -1) {
		routes = append(routes, m[1])
	}
	if len(routes) < 50 {
		t.Fatalf("found only %d owner routes in server.go: the route pattern no longer matches", len(routes))
	}
	reg := fullRegistry(t)
	seen := map[string]bool{}
	var unmapped []string
	for _, r := range routes {
		seen[r] = true
		_, mapped := routeTools[r]
		_, excluded := routeExcluded[r]
		switch {
		case mapped && excluded:
			t.Errorf("%s is both mapped and excluded", r)
		case !mapped && !excluded:
			unmapped = append(unmapped, r)
		}
	}
	if len(unmapped) > 0 {
		sort.Strings(unmapped)
		t.Errorf("owner routes with no tool and no exclusion reason (add a tool, or list them in routeExcluded):\n  %s", strings.Join(unmapped, "\n  "))
	}
	for r, tool := range routeTools {
		if !seen[r] {
			t.Errorf("routeTools names %s, which is not a route in server.go any more", r)
		}
		if reg.Get(tool) == nil {
			t.Errorf("route %s maps to tool %q, which the registry does not have", r, tool)
		}
	}
	for r, why := range routeExcluded {
		if !seen[r] {
			t.Errorf("routeExcluded names %s, which is not a route in server.go any more", r)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("exclusion %s has no reason", r)
		}
	}
}
