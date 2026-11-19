package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// Scenario 1: A recruiter emails you an assessment. Griffin detects it, records the deadline,
// alerts you and adds it to your agenda.
func TestScenario1_RecruiterAssessmentEndToEnd(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "recruiter-assessment@example.com")

	inboxRepo := repository.NewInboxRepository(env.DB.Pool)
	eventsRepo := repository.NewInboundEventRepository(env.DB.Pool)
	triage := &service.InboundTriageService{}
	syncer := &service.GmailSyncer{
		InboxRepo: inboxRepo,
		Events:    eventsRepo,
		Triage:    triage,
		Tasks:     env.TaskRepo,
	}

	refTime := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	syncer.Now = func() time.Time { return refTime }

	emailMsg := domain.EmailMessage{
		ID:        "msg_sarvam_eval_001",
		ThreadID:  "thread_sarvam_001",
		Sender:    "Sarvam AI Recruiting <recruiting@sarvam.ai>",
		Recipient: "Adhyan Jain <adhyanjain2006@gmail.com>",
		Subject:   "Sarvam AI: Technical Assessment Invitation - Applied AI Intern",
		Snippet:   "We are pleased to invite you to take our online Codility assessment...",
		BodyText: `Hi Adhyan,

Thank you for your interest in the Applied AI Intern role at Sarvam AI.
We are pleased to invite you to take our online technical assessment on Codility:
https://app.codility.com/c/run/SARVAM-AI-2027

Please complete the assessment within 48 hours of receiving this email.

Best regards,
The Sarvam AI Talent Team`,
		ReceivedAt: refTime,
	}

	// 1. Ingest email batch via incremental synchronization
	res, err := syncer.ProcessMessages(ctx, u, []domain.EmailMessage{emailMsg})
	require.NoError(t, err)
	assert.Equal(t, 1, res.MessagesSynced)
	require.Len(t, res.AssessmentsFound, 1)

	assessment := res.AssessmentsFound[0]
	assert.Equal(t, "Sarvam AI", assessment.Company)
	assert.Equal(t, "codility", assessment.Platform)
	assert.Equal(t, "https://app.codility.com/c/run/SARVAM-AI-2027", assessment.TestURL)
	assert.Equal(t, domain.AssessmentPending, assessment.Status)
	require.NotNil(t, assessment.Deadline)
	expectedDeadline := refTime.Add(48 * time.Hour)
	assert.WithinDuration(t, expectedDeadline, *assessment.Deadline, time.Second)

	// 2. Verify stored assessment in database
	storedAssessments, err := inboxRepo.ListActiveAssessments(ctx, u, nil)
	require.NoError(t, err)
	require.Len(t, storedAssessments, 1)
	assert.Equal(t, "Sarvam AI", storedAssessments[0].Company)
	assert.Equal(t, "codility", storedAssessments[0].Platform)

	// 3. Verify actionable inbound event created for daily agenda
	actionableEvents, err := eventsRepo.ListActionable(ctx, u, 10)
	require.NoError(t, err)
	require.Len(t, actionableEvents, 1)
	assert.Equal(t, domain.CategoryOpportunity, actionableEvents[0].Category)
	assert.Equal(t, "agenda_add", actionableEvents[0].TriageAction)
	assert.Contains(t, actionableEvents[0].Title, "Sarvam AI")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(actionableEvents[0].Payload, &payload))
	assert.Equal(t, "codility", payload["platform"])
	assert.Equal(t, "https://app.codility.com/c/run/SARVAM-AI-2027", payload["test_url"])
}

// Scenario 2: A matching internship appears on an authorized discovery source. Griffin verifies it,
// checks your actual profile, prepares an application and follows the configured submission policy.
func TestScenario2_JobDiscoveryAndApplicationPolicyEndToEnd(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "job-discovery@example.com")

	discRepo := repository.NewDiscoveryRepository(env.DB.Pool)
	pipeline := &service.OpportunityPipeline{Tasks: env.TaskRepo}
	discovery := &service.JobDiscoveryService{
		DiscoveryRepo: discRepo,
		Pipeline:      pipeline,
	}

	// Mock authorized ATS fetcher
	discovery.CustomFetcher = func(ctx context.Context, source domain.DiscoverySource, company string) ([]service.ATSJobPosting, error) {
		return []service.ATSJobPosting{
			{
				ID:          "modal_sys_001",
				Title:       "Systems & Infrastructure Intern",
				Company:     "Modal Labs",
				URL:         "https://modal.com/careers/systems-intern-2027",
				Description: "Build low-latency distributed container sandboxes and GPU inference clusters in Go.",
				Location:    "Remote / US",
				Source:      domain.SourceGreenhouse,
			},
			{
				ID:          "postman_mkt_002",
				Title:       "Head of Brand Marketing",
				Company:     "Postman",
				URL:         "https://postman.com/careers/marketing",
				Description: "Lead global marketing campaigns and social media channels.",
				Location:    "San Francisco",
				Source:      domain.SourceLever,
			},
		}, nil
	}

	// 1. Execute discovery sweep
	staged, err := discovery.SweepAndStage(ctx, u, []struct {
		Source  domain.DiscoverySource
		Company string
	}{
		{Source: domain.SourceGreenhouse, Company: "modal"},
	})
	require.NoError(t, err)
	// Only the Systems Intern job matches the owner's profile; the marketing job is filtered out
	require.Len(t, staged, 1)
	assert.Equal(t, "Modal Labs", staged[0].Company)
	assert.Equal(t, "Systems & Infrastructure Intern", staged[0].Role)
	assert.Equal(t, "Tier A", staged[0].Tier)
	assert.NotNil(t, staged[0].TaskID)

	// Duplicate check: running discovery again should not create duplicate entries
	stagedAgain, err := discovery.SweepAndStage(ctx, u, []struct {
		Source  domain.DiscoverySource
		Company string
	}{
		{Source: domain.SourceGreenhouse, Company: "modal"},
	})
	require.NoError(t, err)
	assert.Empty(t, stagedAgain, "Duplicate openings must be ignored on subsequent sweeps")

	// 2. Prepare Application and Evaluate Submission Policy
	profRepo := repository.NewOwnerProfileRepository(env.DB.Pool)
	appExec := &service.ApplicationExecutor{
		DiscoveryRepo: discRepo,
		Profiles:      profRepo,
		ReceiptDir:    t.TempDir(),
	}

	// Fail closed: without a stored profile nothing is prepared and nothing is guessed.
	_, err = appExec.ProcessApplication(ctx, u, "Modal Labs", "Systems & Infrastructure Intern", "https://modal.example/careers", nil)
	require.ErrorIs(t, err, service.ErrNoOwnerProfile)
	_, err = profRepo.Save(ctx, u, testOwnerProfile(), "test-fixture")
	require.NoError(t, err)

	// 2A. Standard Form: clean profile fields -> Standing Policy: Auto-Staged
	standardFields := []domain.FormField{
		{Name: "first_name", Label: "First Name", Type: "text", Required: true},
		{Name: "last_name", Label: "Last Name", Type: "text", Required: true},
		{Name: "email", Label: "Email Address", Type: "text", Required: true},
		{Name: "university", Label: "University / College", Type: "text", Required: true},
		{Name: "degree", Label: "Degree / Program", Type: "text", Required: true},
		{Name: "graduation_year", Label: "Expected Graduation Year", Type: "text", Required: true},
		{Name: "github_url", Label: "GitHub Profile", Type: "text", Required: true},
		{Name: "linkedin_url", Label: "LinkedIn Profile", Type: "text", Required: true},
		{Name: "resume", Label: "Resume / CV", Type: "file", Required: true},
	}

	receiptA, err := appExec.ProcessApplication(ctx, u, "Modal Labs", "Systems & Infrastructure Intern", "https://modal.com/careers/systems-intern-2027", standardFields)
	require.NoError(t, err)
	assert.Equal(t, domain.PolicyAutoStaged, receiptA.PolicyDecision)
	// Bullets are verbatim verified facts, each traceable by ID; nothing is invented.
	assert.Contains(t, receiptA.TailoredBullets, "Built a Go scheduler that leases jobs through Redis")
	assert.NotContains(t, receiptA.TailoredBullets, "Rust compiler")
	assert.Equal(t, []string{"fx-sched"}, receiptA.FormPayload["_supporting_fact_ids"])
	assert.Equal(t, "Test", receiptA.FormPayload["first_name"])
	assert.Equal(t, "Candidate", receiptA.FormPayload["last_name"])
	assert.Equal(t, "test.candidate@example.com", receiptA.FormPayload["email"])
	assert.Equal(t, "2028", receiptA.FormPayload["graduation_year"])
	assert.Equal(t, "Candidate_Resume.pdf", receiptA.FormPayload["resume"])
	assert.NotEmpty(t, receiptA.ReceiptHash)
	assert.FileExists(t, receiptA.ReceiptPath)

	// 2B. Form with Legal Clause & Sensitive Question -> Standing Policy: Escalate to 1-Tap Approval
	sensitiveFields := append(standardFields,
		domain.FormField{Name: "arbitration_clause", Label: "Mandatory Binding Arbitration Agreement", Type: "checkbox", Required: true},
		domain.FormField{Name: "disability_status", Label: "Voluntary Self-Identification of Disability", Type: "select", Required: false},
	)

	receiptB, err := appExec.ProcessApplication(ctx, u, "Modal Labs", "Systems & Infrastructure Intern", "https://modal.com/careers/systems-intern-2027", sensitiveFields)
	require.NoError(t, err)
	assert.Equal(t, domain.PolicyEscalate, receiptB.PolicyDecision, "Legal declarations and sensitive questions must escalate")
	reasons, ok := receiptB.FormPayload["_escalation_reasons"].([]string)
	require.True(t, ok)
	require.Len(t, reasons, 3)
	assert.Contains(t, reasons[0], "arbitration")
	assert.Contains(t, reasons[1], "disability")
	assert.Contains(t, reasons[2], "not answerable from verified profile", "an unanswerable required legal field is also flagged")

	// 2C. A required question the profile cannot answer fails closed, and "company name" is
	// never filled with the applicant's own name.
	unknownFields := append(append([]domain.FormField{}, standardFields...),
		domain.FormField{Name: "company_name", Label: "Current Company Name", Type: "text", Required: false},
		domain.FormField{Name: "why", Label: "Describe your favourite tabs-vs-spaces opinion", Type: "textarea", Required: true},
	)
	receiptC, err := appExec.ProcessApplication(ctx, u, "Modal Labs", "Systems & Infrastructure Intern", "https://modal.example/careers", unknownFields)
	require.NoError(t, err)
	assert.Equal(t, domain.PolicyEscalate, receiptC.PolicyDecision)
	assert.NotContains(t, receiptC.FormPayload, "company_name")
	assert.NotContains(t, receiptC.FormPayload, "why")
	cReasons, _ := receiptC.FormPayload["_escalation_reasons"].([]string)
	require.Len(t, cReasons, 1)
	assert.Contains(t, cReasons[0], "tabs-vs-spaces")
}

// Scenario 3: A meaningful event arrives while you're away from your laptop. Griffin decides whether
// to interrupt you, speaks naturally through your phone, and lets you respond conversationally.
func TestScenario3_AttentionInterruptionAndNeuralVoiceEndToEnd(t *testing.T) {
	tempCache := t.TempDir()
	tts := service.NewTTSService(tempCache)
	ctx := context.Background()

	// 1. Audition Masculine Voices
	require.NotEmpty(t, service.AvailableAuditionVoices)
	assert.Equal(t, "ryan", service.AvailableAuditionVoices[0].ID)
	assert.Equal(t, "en-GB-RyanNeural", tts.ResolveVoice("ryan"))
	assert.Equal(t, "en-US-GuyNeural", tts.ResolveVoice("guy"))
	assert.Equal(t, "en-US-ChristopherNeural", tts.ResolveVoice("christopher"))

	// 2. High-Fidelity Speech Generation & Sub-10ms Caching
	speechText := "Adhyan, you just received a 48-hour assessment from Sarvam AI. The deadline is tomorrow at 5 PM."
	audioBytes1, err := tts.Synthesize(ctx, speechText, "ryan")
	if err != nil {
		// External-provider dependent: a failed synthesis is reported as skipped, never as passing
		// (the old behaviour returned fabricated bytes). Replaced by provider-fake tests in the TTS rework.
		t.Skipf("EXTERNAL: neural TTS provider unavailable: %v", err)
	}
	require.NotEmpty(t, audioBytes1)

	// Second request must hit in-memory/disk cache
	audioBytes2, err := tts.Synthesize(ctx, speechText, "ryan")
	require.NoError(t, err)
	assert.True(t, bytes.Equal(audioBytes1, audioBytes2), "Cached audio must match exactly")

	// 3. Attention Engine: Away from laptop with critical assessment deadline -> Spoken Earbud Interrupt
	attentionEngine := &service.AttentionEngine{TTS: tts}
	u := uuid.New()

	// 3A. Away from Laptop with Assessment Deadline
	actionAway, spokenAudio, err := attentionEngine.RouteAlert(ctx, u, "assessment_deadline", domain.HostStateAway, speechText)
	require.NoError(t, err)
	assert.Equal(t, domain.AttentionCriticalInterrupt, actionAway.Level)
	assert.True(t, actionAway.ShouldSpeakVoice, "Away state with critical event must speak voice")
	assert.True(t, actionAway.ShouldInterrupt, "Critical event must interrupt")
	assert.Equal(t, "phone_earbuds", actionAway.PreferredOutput)
	assert.NotEmpty(t, spokenAudio)

	// 3B. Away from Laptop with Standard Opportunity -> Quiet Push, No Audio Interruption
	actionHigh, spokenHigh, err := attentionEngine.RouteAlert(ctx, u, "high", domain.HostStateAway, "New job opening at Modal")
	require.NoError(t, err)
	assert.Equal(t, domain.AttentionHighPriorityBrief, actionHigh.Level)
	assert.False(t, actionHigh.ShouldSpeakVoice, "Standard high priority alert should not interrupt voice when away")
	assert.Empty(t, spokenHigh)

	// 3C. In-Meeting with Critical Event -> Suppress Spoken Audio to Avoid Disturbance
	actionMeeting, spokenMeeting, err := attentionEngine.RouteAlert(ctx, u, "critical", domain.HostStateInMeeting, speechText)
	require.NoError(t, err)
	assert.False(t, actionMeeting.ShouldSpeakVoice, "In-meeting events must never speak out loud")
	assert.Equal(t, "silent_push", actionMeeting.PreferredOutput)
	assert.Empty(t, spokenMeeting)

	_ = os.RemoveAll(tempCache)
}
