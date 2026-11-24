package research

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// stubPlaybook is a playbook with domain wording, to see where the engine uses it.
type stubPlaybook struct{ processed *domain.ResearchRun }

func (stubPlaybook) Name() string        { return "company_intelligence" }
func (stubPlaybook) Description() string { return "stub" }
func (stubPlaybook) BuildRequest(map[string]any) (domain.ResearchRequest, error) {
	return domain.ResearchRequest{}, nil
}
func (stubPlaybook) QueryTemplates() []string          { return []string{"%s funding investors"} }
func (stubPlaybook) Subject(ctx map[string]any) string { return playbooks.StringIn(ctx, "company") }
func (stubPlaybook) DefaultQuestions() []string        { return []string{"What is the funding?"} }
func (stubPlaybook) PlanGuidance() string              { return "STUB GUIDANCE company wording" }
func (p *stubPlaybook) ProcessResults(_ context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error) {
	p.processed = run
	return &playbooks.PlaybookResult{Summary: "ok"}, nil
}

func TestGenericRunWithNoPlaybookUsesNoDomainWording(t *testing.T) {
	s := newScenario()
	s.llm.extract[aboutURL] = aboutReply("AcmeProcure announced a $10M Series A")
	require.Nil(t, s.eng.Playbooks, "no registry")
	run := s.start(t, domain.ResearchRequest{Objective: "Research procurement tech startups", Context: map[string]any{"company": "Acme"}})
	done := s.finish(t, run.ID)
	require.Equal(t, domain.PhaseCompleted, done.Status)

	plan := s.llm.last("plan")
	require.NotContains(t, plan.system, "Domain guidance")
	for _, w := range []string{"traction", "funding investors", "careers jobs hiring", "tech stack"} {
		require.NotContains(t, strings.ToLower(plan.system), w, "the generic engine carries no company wording")
	}
	for _, q := range done.ExecutedQueries {
		require.NotContains(t, q, "funding investors")
	}
	res, err := s.eng.ProcessPlaybook(context.Background(), done)
	require.NoError(t, err)
	require.Nil(t, res, "a run with no playbook has nothing to process")
}

func TestRegisteredPlaybookSuppliesWordingQuestionsAndOnlySupportedFindings(t *testing.T) {
	s := newScenario()
	pb := &stubPlaybook{}
	s.eng.Playbooks = playbooks.Registry{"company_intelligence": pb}
	s.llm.extract[aboutURL] = aboutReply("AcmeProcure announced a $10M Series A")
	run := s.start(t, domain.ResearchRequest{Playbook: "company_intelligence", Objective: "Research Acme", Context: map[string]any{"company": "Acme"}})
	require.Len(t, run.Plan.Questions, 1, "the playbook's default question is used when the request names none")
	require.Equal(t, "What is the funding?", run.Plan.Questions[0].Question)

	done := s.finish(t, run.ID)
	require.Contains(t, s.llm.last("plan").system, "STUB GUIDANCE")
	require.Contains(t, done.Plan.InitialQueries, "Acme funding investors")

	done.Findings = append(done.Findings,
		domain.ResearchFinding{ID: "u", Status: domain.EvidenceUnverified},
		domain.ResearchFinding{ID: "c", Status: domain.EvidenceConflicting})
	_, err := s.eng.ProcessPlaybook(context.Background(), done)
	require.NoError(t, err)
	require.NotNil(t, pb.processed)
	for _, f := range pb.processed.Findings {
		require.Equal(t, domain.EvidenceSupported, f.Status, "the playbook sees only supported findings")
	}

	// an unfinished run is not processed
	pb.processed = nil
	active := *done
	active.Status = domain.PhaseSourceDiscovery
	_, err = s.eng.ProcessPlaybook(context.Background(), &active)
	require.NoError(t, err)
	require.Nil(t, pb.processed)
}
