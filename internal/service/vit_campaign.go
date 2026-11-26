package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// VITCampaignService builds the outreach queue from the owner's own people records and runs its lifecycle.
type VITCampaignService struct {
	pool         database.Querier
	peopleRepo   *repository.PeopleRepository
	campaignRepo *repository.CampaignRepository
	owner        OwnerProfileSource
}

// NewVITCampaignService constructs a VITCampaignService.
func NewVITCampaignService(pool database.Querier, peopleRepo *repository.PeopleRepository, campaignRepo *repository.CampaignRepository) *VITCampaignService {
	return &VITCampaignService{
		pool:         pool,
		peopleRepo:   peopleRepo,
		campaignRepo: campaignRepo,
	}
}

// OwnerProfileSource loads the owner's profile; it is nil-safe (no profile means placeholders in drafts).
type OwnerProfileSource interface {
	Latest(ctx context.Context, userID uuid.UUID) (*domain.OwnerProfile, error)
}

// SetOwnerProfiles gives the service the owner profile that drafts take the sender's facts from.
func (s *VITCampaignService) SetOwnerProfiles(src OwnerProfileSource) { s.owner = src }

// OwnerProfileOrEmpty returns the owner's latest profile, or an empty one when there is none.
func OwnerProfileOrEmpty(ctx context.Context, src OwnerProfileSource, userID uuid.UUID) (domain.OwnerProfile, error) {
	if src == nil {
		return domain.OwnerProfile{}, nil
	}
	p, err := src.Latest(ctx, userID)
	if err != nil || p == nil {
		return domain.OwnerProfile{}, err
	}
	return *p, nil
}

// queueScanLimit bounds how many of the owner's people one build reads.
const queueScanLimit = 100000

// BuildQueue turns the owner's own people records into outreach targets. It invents nothing: a person is queued only
// when confirmed as VIT Vellore and eligible by graduation date, is not do-not-contact and has no excluded employer.
// Pay potential comes only from stored company pay observations; with none it is LOW/UNKNOWN and excludes no one.
// Everyone else stays on the Alumni page for review.
func (s *VITCampaignService) BuildQueue(ctx context.Context, userID uuid.UUID) (*domain.VITQueueBuild, error) {
	campaign, err := s.campaignRepo.EnsureVITAlumniCampaign(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ensure campaign: %w", err)
	}
	sender, err := OwnerProfileOrEmpty(ctx, s.owner, userID)
	if err != nil {
		return nil, err
	}
	pay, err := s.campaignRepo.CompanyPayEvidence(ctx, userID)
	if err != nil {
		return nil, err
	}
	people, err := s.peopleRepo.List(ctx, userID, repository.PersonFilter{Limit: queueScanLimit})
	if err != nil {
		return nil, err
	}

	res := &domain.VITQueueBuild{Considered: len(people)}
	for _, p := range people {
		if p.DoNotContact || p.ContactState == domain.ContactOptedOut {
			res.SkippedDNC++
			continue
		}
		if excluded, _ := domain.IsExcludedEmployer(p.Company); excluded {
			res.SkippedExcluded++
			continue
		}
		if p.GraduationEligibility != domain.CohortEligible || p.VITAffiliation != domain.AffiliationConfirmedVellore {
			res.SkippedUnverified++
			continue
		}
		if p.ProfileURL == nil || *p.ProfileURL == "" {
			res.SkippedNoProfile++
			continue
		}

		payPotential := domain.PayPotentialLowUnknown
		if cp, ok := repository.PayFor(pay, p.CurrentCompanyID, p.Company); ok {
			payPotential = domain.EvaluatePayPotential(cp.Advertised, cp.EmployeeReported)
		}
		tier, score, rationale := domain.EvaluatePriorityTier(p.ACMAffiliation, payPotential, p.Company, true, true, false)
		if err := s.peopleRepo.UpdatePriority(ctx, userID, p.ID, tier, score, rationale); err != nil {
			return nil, err
		}
		p.PriorityTier, p.PriorityScore, p.PriorityRationale = tier, score, rationale

		draft, err := GenerateVITAlumniDraft(p, sender)
		if err != nil {
			continue
		}
		status := domain.TargetStatusDrafted
		if draft.NeedsUserReview {
			status = domain.TargetStatusNeedsUserReview
		}
		target := &domain.VITCampaignTarget{
			CampaignID:            campaign.ID,
			PersonID:              p.ID,
			Name:                  p.Name,
			ProfileURL:            *p.ProfileURL,
			Company:               p.Company,
			Title:                 p.Title,
			Campus:                p.Campus,
			GraduationYear:        p.GraduationYear,
			GraduationEligibility: p.GraduationEligibility,
			ACMAffiliation:        p.ACMAffiliation,
			ACMRole:               p.ACMRole,
			PriorityTier:          tier,
			PriorityScore:         score,
			PriorityRationale:     rationale,
			PayPotential:          payPotential,
			ExtendedStatus:        status,
			ShortNote:             draft.ShortNote,
			AlternativeDraft:      draft.WarmDraft,
			SelectionReason:       draft.SelectionReason,
			PersonalisationEvidence: map[string]any{
				"verified_facts": draft.VerifiedFacts,
				"excluded_facts": draft.ExcludedFacts,
				"pay_potential":  payPotential,
			},
		}
		if err := s.campaignRepo.UpsertVITCampaignTarget(ctx, userID, target); err != nil {
			return nil, err
		}
		res.Queued++
		if draft.NeedsUserReview {
			res.NeedsUserReview++
		}
	}

	if res.Stats, err = s.campaignRepo.GetCampaignStats(ctx, userID, campaign.ID); err != nil {
		return nil, err
	}
	return res, nil
}

// GetCampaignQueue returns the prioritised pilot queue (~25 targets by default, ordered P1 > P2 > P3 > P4).
func (s *VITCampaignService) GetCampaignQueue(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.VITCampaignTarget, error) {
	campaign, err := s.campaignRepo.EnsureVITAlumniCampaign(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get campaign: %w", err)
	}

	if limit <= 0 {
		limit = 25 // Default to staged pilot batch size
	}

	return s.campaignRepo.ListVITCampaignTargets(ctx, userID, campaign.ID, limit)
}

// GetCampaignStats returns the live cohort metrics.
func (s *VITCampaignService) GetCampaignStats(ctx context.Context, userID uuid.UUID) (*domain.VITCampaignStats, error) {
	campaign, err := s.campaignRepo.EnsureVITAlumniCampaign(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get campaign: %w", err)
	}
	return s.campaignRepo.GetCampaignStats(ctx, userID, campaign.ID)
}

// UpdateTargetAction transitions a target's status in response to user action (approve, sent, reply, dnc).
func (s *VITCampaignService) UpdateTargetAction(ctx context.Context, userID, targetID uuid.UUID, action string) error {
	var nextStatus string
	switch action {
	case "approve":
		nextStatus = domain.TargetStatusApproved
	case "sent_manually":
		nextStatus = domain.TargetStatusSentByUser
	case "reply":
		nextStatus = domain.TargetStatusReplied
	case "dnc":
		nextStatus = domain.TargetStatusDoNotContact
	default:
		return fmt.Errorf("%w: unrecognized action %s", domain.ErrValidation, action)
	}

	return s.campaignRepo.UpdateTargetStatus(ctx, userID, targetID, nextStatus)
}

// GetLinkedInQueries returns copy-ready search queries for manual LinkedIn exploration.
func (s *VITCampaignService) GetLinkedInQueries() []domain.LinkedInSearchQuery {
	return domain.GenerateLinkedInQueries()
}
