package service

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/migrations"
)

// matrixPrompts routes one representative request to each of the 11 managers via
// the production keyword classifier (classifyIntent).
var matrixPrompts = map[string]string{
	"career":            "find me a job and tailor my resume",
	"research":          "survey the literature on vector databases",
	"engineering":       "fix the bug in the repo",
	"hackathon":         "find a hackathon to enter",
	"freelance":         "find a freelance gig",
	"networking":        "plan crm outreach",
	"tech_intelligence": "summarize this week's ai release news",
	"learning":          "build a study curriculum for Go",
	"personal_ops":      "organize my calendar agenda",
	"finance":           "audit my aws bill",
	"product_ideas":     "draft a prd for a new idea",
}

// TestManagerMatrix_EveryManagerRoutesAndYieldsValidPlan proves, for each registered
// manager, that (1) a keyword prompt routes to it, (2) the rule-based plan passes
// domain.ValidatePlan, and (3) every planned WorkerKey belongs to the manager's
// AvailableWorkers (no orphan worker keys in plans).
func TestManagerMatrix_EveryManagerRoutesAndYieldsValidPlan(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	allowed := map[string]bool{}
	for _, d := range reg.List() {
		allowed[d.ID] = true
	}
	if len(matrixPrompts) != len(allowed) {
		t.Fatalf("matrix covers %d managers, registry has %d", len(matrixPrompts), len(allowed))
	}

	planner := NewRuleBasedPlanner()
	for id, prompt := range matrixPrompts {
		t.Run(id, func(t *testing.T) {
			if got := classifyIntent(strings.ToLower(prompt)); got != id {
				t.Fatalf("prompt %q routed to %q, want %q", prompt, got, id)
			}
			plan, err := planner.GeneratePlan(context.Background(), &domain.UserRequest{
				RequestText: prompt, Priority: domain.TaskPriorityNormalP2,
			}, reg)
			if err != nil {
				t.Fatalf("GeneratePlan: %v", err)
			}
			if plan.ManagerID != id {
				t.Fatalf("plan manager %q, want %q", plan.ManagerID, id)
			}
			if errs := domain.ValidatePlan(plan, allowed); len(errs) > 0 {
				t.Fatalf("plan invalid: %v", errs)
			}
			mgr, _ := reg.Get(id)
			avail := map[string]bool{}
			for _, w := range mgr.Definition().AvailableWorkers {
				avail[w] = true
			}
			for _, ct := range plan.ChildTasks {
				if ct.WorkerKey == "" || !avail[ct.WorkerKey] {
					t.Errorf("child %s worker %q not in AvailableWorkers %v", ct.Key, ct.WorkerKey, mgr.Definition().AvailableWorkers)
				}
			}
		})
	}
}

// TestManagerMatrix_ApprovalOperationsAreGated proves that every manager whose
// AllowedCapabilities include a sensitive capability yields a plan child with
// ApprovalRequired=true, and that managers without one do not.
func TestManagerMatrix_ApprovalOperationsAreGated(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	planner := NewRuleBasedPlanner()
	for id, prompt := range matrixPrompts {
		mgr, _ := reg.Get(id)
		def := mgr.Definition()
		sensitive, _ := domain.RequiresApproval(def.AllowedCapabilities, "")
		plan, err := planner.GeneratePlan(context.Background(), &domain.UserRequest{RequestText: prompt}, reg)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		anyApproval := false
		for _, ct := range plan.ChildTasks {
			anyApproval = anyApproval || ct.ApprovalRequired
		}
		t.Logf("%-18s sensitive_caps=%-5v plan_has_approval_child=%v", id, sensitive, anyApproval)
		// Career/engineering use bespoke decompositions: career only gates on "send"/"apply"
		// keywords, engineering always gates its write step.
		switch id {
		case "career":
			if anyApproval {
				t.Errorf("career plan without send/apply keyword should not require approval")
			}
		case "engineering":
			if !anyApproval {
				t.Errorf("engineering write step must require approval")
			}
		case "research":
			if anyApproval {
				t.Errorf("research has no sensitive capability")
			}
		default:
			if sensitive != anyApproval {
				t.Errorf("%s: sensitive=%v but plan approval=%v", id, sensitive, anyApproval)
			}
		}
	}
}

// TestManagerMatrix_WorkerKeysUniqueAndUnseeded proves AvailableWorkers keys are unique
// across managers, and that NO migration seeds worker_definitions: definitions exist
// only when a user creates them via the API, so planned WorkerKeys fall back to a
// plain task in delegatePlan on a fresh database.
func TestManagerMatrix_WorkerKeysUniqueAndUnseeded(t *testing.T) {
	seen := map[string]string{}
	for _, d := range manager.NewDefaultRegistry().List() {
		for _, w := range d.AvailableWorkers {
			if prev, dup := seen[w]; dup {
				t.Errorf("worker key %q declared by both %s and %s", w, prev, d.ID)
			}
			seen[w] = d.ID
		}
	}
	if len(seen) != 25 {
		t.Errorf("expected 25 declared worker keys, got %d", len(seen))
	}

	err := fs.WalkDir(migrations.FS, ".", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.HasSuffix(p, ".sql") {
			return err
		}
		b, rerr := fs.ReadFile(migrations.FS, p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(strings.ToLower(string(b)), "insert into worker_definitions") {
			t.Errorf("migration %s seeds worker_definitions; update the capability matrix", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
