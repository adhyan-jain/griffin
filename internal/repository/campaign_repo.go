package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// CampaignRepository manages campaigns, targets, and priority outreach queues.
type CampaignRepository struct {
	pool database.Querier
}

// NewCampaignRepository constructs a CampaignRepository.
func NewCampaignRepository(pool database.Querier) *CampaignRepository {
	return &CampaignRepository{pool: pool}
}

const vitCampaignName = "VIT Alumni Prospecting & Startup Network Campaign"

// EnsureVITAlumniCampaign returns or creates the singleton VIT alumni campaign for a user.
func (r *CampaignRepository) EnsureVITAlumniCampaign(ctx context.Context, userID uuid.UUID) (*domain.OutreachCampaign, error) {
	var c domain.OutreachCampaign
	limitsJSON, _ := json.Marshal(domain.DefaultOutreachLimits("linkedin"))

	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, name, goal, channel, status, limits, created_at, updated_at
		FROM outreach_campaigns
		WHERE user_id = $1 AND name = $2`,
		userID, vitCampaignName,
	).Scan(&c.ID, &c.UserID, &c.Name, &c.Goal, &c.Channel, &c.Status, &c.Limits, &c.CreatedAt, &c.UpdatedAt)

	if err == nil {
		return &c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("find campaign: %w", err)
	}

	goal := "Prospect VIT Vellore alumni (graduated <= 2022) prioritising ACM-VIT, match with high-potential startups, and prepare friendly outreach."
	err = r.pool.QueryRow(ctx, `
		INSERT INTO outreach_campaigns (user_id, name, goal, channel, status, limits, campaign_type)
		VALUES ($1, $2, $3, 'linkedin', 'active', $4::jsonb, 'vit_alumni')
		RETURNING id, user_id, name, goal, channel, status, limits, created_at, updated_at`,
		userID, vitCampaignName, goal, string(limitsJSON),
	).Scan(&c.ID, &c.UserID, &c.Name, &c.Goal, &c.Channel, &c.Status, &c.Limits, &c.CreatedAt, &c.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("create campaign: %w", err)
	}
	return &c, nil
}

// alreadyContacted reports whether this person (or the same LinkedIn handle) was already messaged from another
// campaign of the same user, so one human is never approached twice.
func alreadyContacted(ctx context.Context, q database.Querier, userID, campaignID, personID uuid.UUID, handle string) (bool, error) {
	var dup bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM outreach_targets o
			WHERE o.user_id = $1 AND o.campaign_id <> $2
			  AND (o.status IN ('sent','replied') OR o.sent_at IS NOT NULL
			       OR o.extended_status IN ('SENT_BY_USER','REPLIED','FOLLOW_UP'))
			  AND (o.person_id = $3
			       OR ($4 <> '' AND lower(rtrim(trim(o.handle), '/')) = lower(rtrim(trim($4), '/'))))
		)`, userID, campaignID, personID, handle).Scan(&dup)
	return dup, err
}

// UpsertVITCampaignTarget adds or refreshes an enriched target in the outreach queue. Targets that are already
// approved, sent or closed are never touched, and a person already messaged from another campaign is skipped.
func (r *CampaignRepository) UpsertVITCampaignTarget(ctx context.Context, userID uuid.UUID, t *domain.VITCampaignTarget) error {
	if t == nil || t.CampaignID == uuid.Nil || t.PersonID == uuid.Nil {
		return fmt.Errorf("%w: campaign_id and person_id required", domain.ErrValidation)
	}

	dup, err := alreadyContacted(ctx, r.pool, userID, t.CampaignID, t.PersonID, t.ProfileURL)
	if err != nil {
		return fmt.Errorf("check duplicate outreach: %w", err)
	}
	if dup {
		return nil
	}

	evidenceJSON, _ := json.Marshal(t.PersonalisationEvidence)
	targetStatus := domain.OutreachDrafted
	if t.DoNotContact {
		targetStatus = domain.OutreachClosed
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO outreach_targets (
			campaign_id, user_id, name, handle, info, status, draft,
			person_id, priority_tier, alternative_draft, selection_reason,
			personalisation_evidence, extended_status
		) VALUES (
			$1, (SELECT user_id FROM outreach_campaigns WHERE id = $1 AND user_id = $13), $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11::jsonb, $12
		)
		ON CONFLICT (campaign_id, handle) DO UPDATE SET
			name = EXCLUDED.name,
			info = EXCLUDED.info,
			draft = EXCLUDED.draft,
			priority_tier = EXCLUDED.priority_tier,
			alternative_draft = EXCLUDED.alternative_draft,
			selection_reason = EXCLUDED.selection_reason,
			personalisation_evidence = EXCLUDED.personalisation_evidence,
			extended_status = EXCLUDED.extended_status,
			updated_at = NOW()
		WHERE outreach_targets.user_id = $13
		  AND outreach_targets.extended_status IN ('DISCOVERED','VERIFIED','QUALIFIED','PRIORITISED','DRAFTED','NEEDS_USER_REVIEW')`,
		t.CampaignID, t.Name, t.ProfileURL, t.Title+" at "+t.Company, string(targetStatus), t.ShortNote,
		t.PersonID, t.PriorityTier, t.AlternativeDraft, t.SelectionReason,
		string(evidenceJSON), t.ExtendedStatus, userID,
	)
	if err != nil {
		return fmt.Errorf("upsert campaign target: %w", err)
	}
	return nil
}

// ListVITCampaignTargets returns targets in strict priority order (P1 > P2 > P3 > P4 > REVIEW).
func (r *CampaignRepository) ListVITCampaignTargets(ctx context.Context, userID, campaignID uuid.UUID, limit int) ([]*domain.VITCampaignTarget, error) {
	if limit <= 0 {
		limit = 50
	}

	q := `
		SELECT 
			t.id, t.campaign_id, t.person_id, t.name, t.handle,
			coalesce(p.metadata->>'company', '') as company,
			coalesce(p.metadata->>'title', '') as title,
			p.campus,
			p.graduation_year,
			p.graduation_eligibility as grad_elig,
			p.acm_vit_affiliation as acm_affil,
			p.acm_role as acm_role,
			p.priority_tier as tier,
			p.priority_score as score,
			p.priority_rationale as rationale,
			coalesce(t.personalisation_evidence->>'pay_potential', 'LOW/UNKNOWN') as pay_pot,
			t.draft,
			coalesce(t.alternative_draft, '') as alt_draft,
			coalesce(t.selection_reason, '') as sel_reason,
			coalesce(t.extended_status, 'DRAFTED') as ext_status,
			p.do_not_contact,
			t.sent_at, t.replied_at, t.created_at, t.updated_at
		FROM outreach_targets t
		JOIN canonical_entities p ON p.id = t.person_id
		WHERE t.campaign_id = $1 AND t.user_id = $2 AND p.user_id = $2
		ORDER BY 
			CASE p.priority_tier
				WHEN 'P1' THEN 1
				WHEN 'P2' THEN 2
				WHEN 'P3' THEN 3
				WHEN 'P4' THEN 4
				ELSE 5
			END ASC,
			p.priority_score DESC,
			t.created_at DESC
		LIMIT $3`

	rows, err := r.pool.Query(ctx, q, campaignID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list campaign targets: %w", err)
	}
	defer rows.Close()

	var targets []*domain.VITCampaignTarget
	for rows.Next() {
		var (
			t                                                   domain.VITCampaignTarget
			pID                                                 *uuid.UUID
			gradYear                                            *int
			draft, altDraft, selReason, extStatus               string
			company, title, campus, gradElig, acmAffil, acmRole string
			tier, rationale, payPot                             string
			score                                               int
		)

		err := rows.Scan(
			&t.ID, &t.CampaignID, &pID, &t.Name, &t.ProfileURL,
			&company, &title, &campus, &gradYear, &gradElig, &acmAffil, &acmRole,
			&tier, &score, &rationale, &payPot,
			&draft, &altDraft, &selReason, &extStatus,
			&t.DoNotContact, &t.SentAt, &t.RepliedAt, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}

		if pID != nil {
			t.PersonID = *pID
		}
		t.Company = company
		t.Title = title
		t.Campus = campus
		t.GraduationYear = gradYear
		t.GraduationEligibility = gradElig
		t.ACMAffiliation = acmAffil
		t.ACMRole = acmRole
		t.PriorityTier = tier
		t.PriorityScore = score
		t.PriorityRationale = rationale
		t.PayPotential = payPot
		t.ShortNote = draft
		t.AlternativeDraft = altDraft
		t.SelectionReason = selReason
		t.ExtendedStatus = extStatus

		targets = append(targets, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list campaign targets: %w", err)
	}
	return targets, nil
}

// UpdateTargetStatus records human-in-the-loop actions (approve, send, reply, dnc).
// Strictly fails closed if the target is marked DNC.
func (r *CampaignRepository) UpdateTargetStatus(ctx context.Context, userID, targetID uuid.UUID, nextStatus string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		current, handle, personState string
		campaignID                   uuid.UUID
		isDNC                        bool
		personID                     *uuid.UUID
	)
	err = tx.QueryRow(ctx, `
		SELECT t.extended_status, t.person_id, coalesce(p.do_not_contact, false), coalesce(p.contact_state, ''),
		       t.campaign_id, t.handle
		FROM outreach_targets t
		LEFT JOIN canonical_entities p ON p.id = t.person_id AND p.user_id = t.user_id
		WHERE t.id = $1 AND t.user_id = $2
		FOR UPDATE OF t`, targetID, userID,
	).Scan(&current, &personID, &isDNC, &personState, &campaignID, &handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check target: %w", err)
	}
	if personState == string(domain.ContactOptedOut) {
		isDNC = true
	}

	if err := domain.ValidateTargetTransition(current, nextStatus, isDNC); err != nil {
		return err
	}

	if nextStatus == domain.TargetStatusApproved {
		pid := uuid.Nil
		if personID != nil {
			pid = *personID
		}
		dup, err := alreadyContacted(ctx, tx, userID, campaignID, pid, handle)
		if err != nil {
			return fmt.Errorf("check duplicate outreach: %w", err)
		}
		if dup {
			return fmt.Errorf("%w: this person was already messaged from another campaign", domain.ErrConflict)
		}
	}

	now := time.Now().UTC()
	var sentAt, repliedAt *time.Time
	var baseStatus domain.OutreachStatus = domain.OutreachDrafted

	switch nextStatus {
	case domain.TargetStatusApproved:
		baseStatus = domain.OutreachApproved
	case domain.TargetStatusSentByUser:
		baseStatus = domain.OutreachSent
		sentAt = &now
	case domain.TargetStatusReplied:
		baseStatus = domain.OutreachReplied
		repliedAt = &now
	case domain.TargetStatusDoNotContact, domain.TargetStatusOptedOut:
		baseStatus = domain.OutreachClosed
	}

	_, err = tx.Exec(ctx, `
		UPDATE outreach_targets SET
			status = $2,
			extended_status = $3,
			sent_at = COALESCE($4, sent_at),
			replied_at = COALESCE($5, replied_at),
			updated_at = $6
		WHERE id = $1 AND user_id = $7`,
		targetID, string(baseStatus), nextStatus, sentAt, repliedAt, now, userID,
	)
	if err != nil {
		return fmt.Errorf("update target status: %w", err)
	}

	// Synchronize with person entity if present
	if personID != nil && *personID != uuid.Nil {
		var personState domain.ContactState
		var setDNC *bool

		switch nextStatus {
		case domain.TargetStatusApproved:
			personState = domain.ContactApproved
		case domain.TargetStatusSentByUser:
			personState = domain.ContactSentManually
		case domain.TargetStatusReplied:
			personState = domain.ContactReplied
		case domain.TargetStatusDoNotContact, domain.TargetStatusOptedOut:
			personState = domain.ContactOptedOut
			t := true
			setDNC = &t
		}

		if personState != "" {
			if _, err := tx.Exec(ctx, `
				UPDATE canonical_entities SET
					contact_state = $2,
					do_not_contact = COALESCE($3, do_not_contact),
					updated_at = NOW()
				WHERE id = $1 AND user_id = $4`,
				*personID, string(personState), setDNC, userID,
			); err != nil {
				return fmt.Errorf("sync person state: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// payEvidenceSQL lists the company observations that count as pay evidence.
const payEvidenceSQL = `('` + domain.PropAdvertisedPay + `', '` + domain.PropEmployeeReportedPay + `')`

// CompanyPay is the pay text stored for one company: advertised and employee-reported observations, joined.
type CompanyPay struct {
	Advertised       string
	EmployeeReported string
}

// CompanyPayEvidence returns the owner's stored pay observations per company, keyed both by the company id
// string and by its normalized name (domain.NormalizeEmployer), so a person is matched by either.
func (r *CampaignRepository) CompanyPayEvidence(ctx context.Context, userID uuid.UUID) (map[string]CompanyPay, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id::text, c.normalized_name, o.property, o.value
		FROM company_observations o
		JOIN companies c ON c.id = o.company_id AND c.user_id = o.user_id
		WHERE o.user_id = $1 AND o.property IN `+payEvidenceSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("company pay evidence: %w", err)
	}
	defer rows.Close()
	out := map[string]CompanyPay{}
	for rows.Next() {
		var id, norm, prop, val string
		if err := rows.Scan(&id, &norm, &prop, &val); err != nil {
			return nil, fmt.Errorf("company pay evidence: %w", err)
		}
		for _, k := range []string{id, norm} {
			cp := out[k]
			if prop == domain.PropAdvertisedPay {
				cp.Advertised += " " + val
			} else {
				cp.EmployeeReported += " " + val
			}
			out[k] = cp
		}
	}
	return out, rows.Err()
}

// PayFor returns the stored pay text for a person's current company, matched by company id or normalized name.
func PayFor(m map[string]CompanyPay, companyID *uuid.UUID, company string) (CompanyPay, bool) {
	if companyID != nil {
		if cp, ok := m[companyID.String()]; ok {
			return cp, true
		}
	}
	if n := domain.NormalizeEmployer(company); n != "" {
		if cp, ok := m[n]; ok {
			return cp, true
		}
	}
	return CompanyPay{}, false
}

// GetCampaignStats counts the owner's people records and this campaign's targets. Every count is of stored rows.
func (r *CampaignRepository) GetCampaignStats(ctx context.Context, userID, campaignID uuid.UUID) (*domain.VITCampaignStats, error) {
	stats := &domain.VITCampaignStats{
		LastRefreshedAt: time.Now().UTC(),
	}
	pay, err := r.CompanyPayEvidence(ctx, userID)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT vit_affiliation, graduation_eligibility, acm_vit_affiliation, professional_function, priority_tier,
		       (public_contact_route <> '' OR public_business_email <> ''),
		       do_not_contact, current_company_id, coalesce(metadata->>'company', '')
		FROM canonical_entities
		WHERE user_id = $1 AND kind = 'person'`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("campaign stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			vitAffil, gradElig, acmAffil, fn, tier, company string
			contactable, dnc                                bool
			companyID                                       *uuid.UUID
		)
		if err := rows.Scan(&vitAffil, &gradElig, &acmAffil, &fn, &tier, &contactable, &dnc, &companyID, &company); err != nil {
			return nil, fmt.Errorf("campaign stats: %w", err)
		}

		stats.TotalPeople++
		if vitAffil == domain.AffiliationConfirmedVellore {
			stats.ConfirmedVellore++
		}
		switch acmAffil {
		case "confirmed":
			stats.ConfirmedACM++
		case "likely":
			stats.LikelyACM++
		}
		switch gradElig {
		case domain.CohortEligible:
			stats.EligibleCohort++
		case domain.CohortBorderline2022:
			stats.Borderline2022++
		}
		if vitAffil != domain.AffiliationConfirmedVellore || gradElig != domain.CohortEligible {
			stats.Unverified++
		}
		switch fn {
		case "technology":
			stats.TechRoles++
		case "operations":
			stats.OpsRoles++
		}
		if _, ok := PayFor(pay, companyID, company); ok {
			stats.WithPayEvidence++
		}
		if contactable {
			stats.PubliclyContactable++
		}
		if dnc {
			stats.DoNotContactCount++
		}
		switch tier {
		case domain.PriorityP1:
			stats.TierP1Count++
		case domain.PriorityP2:
			stats.TierP2Count++
		case domain.PriorityP3:
			stats.TierP3Count++
		case domain.PriorityP4:
			stats.TierP4Count++
		case domain.PriorityReview:
			stats.TierReviewCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("campaign stats: %w", err)
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE extended_status = 'DRAFTED'),
			count(*) FILTER (WHERE extended_status = 'NEEDS_USER_REVIEW'),
			count(*) FILTER (WHERE extended_status = 'APPROVED'),
			count(*) FILTER (WHERE extended_status = 'SENT_BY_USER'),
			count(*) FILTER (WHERE extended_status = 'REPLIED')
		FROM outreach_targets
		WHERE campaign_id = $1 AND user_id = $2`,
		campaignID, userID,
	).Scan(&stats.DraftsReady, &stats.NeedsUserReview, &stats.Approved, &stats.MessagesSentManually, &stats.RepliesReceived); err != nil {
		return nil, fmt.Errorf("campaign target stats: %w", err)
	}

	return stats, nil
}
