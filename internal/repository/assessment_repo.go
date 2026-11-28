package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/domain"
)

// Assessments are read and written through OpportunityRepository. Production reaches Postgres in the simple
// protocol: every argument is a scalar, string or time, and JSON goes in as text cast to jsonb.

// prefixed turns "id, source" into "j.id, j.source".
func prefixed(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// AssessmentSubjects returns the owner's open listings with the keys of their current assessment, newest first and
// unassessed ones first. Descriptions are not loaded: read one with ListingDescription when it is assessed.
func (r *OpportunityRepository) AssessmentSubjects(ctx context.Context, userID uuid.UUID) ([]domain.AssessmentSubject, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixed("j.", listingCols)+`, COALESCE(c.kind, 'unknown'), a.assessment_key, a.input_hash, a.assessor_version
		FROM job_discoveries j
		LEFT JOIN companies c ON c.id = j.company_id
		LEFT JOIN listing_assessments a ON a.listing_id = j.id
		WHERE j.user_id = $1 AND j.stage <> 'closed' AND j.closed_at IS NULL
		ORDER BY (a.listing_id IS NULL) DESC, j.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("assessment subjects: %w", err)
	}
	defer rows.Close()
	var out []domain.AssessmentSubject
	for rows.Next() {
		l := &domain.Listing{UserID: userID}
		var kind string
		var key, input *string
		var version *int
		if err := rows.Scan(append(scanTargets(l), &kind, &key, &input, &version)...); err != nil {
			return nil, fmt.Errorf("scan assessment subject: %w", err)
		}
		s := domain.AssessmentSubject{Listing: l, CompanyKind: kind, Assessed: key != nil}
		if key != nil && input != nil && version != nil {
			s.Key, s.InputHash, s.Version = *key, *input, *version
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListingDescription returns a listing's stored description ("" when it has none; aggregator leads do not).
func (r *OpportunityRepository) ListingDescription(ctx context.Context, userID, id uuid.UUID) (string, error) {
	var d string
	err := r.pool.QueryRow(ctx, `SELECT description FROM job_discoveries WHERE user_id = $1 AND id = $2`, userID, id).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("listing description: %w", err)
	}
	return d, nil
}

const assessmentCols = `a.listing_id, a.description_hash, a.assessment_key, a.input_hash, a.assessor_version, a.employment_class, a.role_class, a.eligibility,
	a.eligibility_reasons::text, a.location_fit, a.grad_year_fit, a.compensation_monthly_inr::float8, a.duration, a.skills_matched::text, a.value_score,
	a.value_breakdown::text, a.value_class, a.assessed_at, a.model_used, a.model_key, a.model_output::text`

// assessmentRow holds the nullable columns of a left-joined assessment.
type assessmentRow struct {
	listingID                                     *uuid.UUID
	descHash, key, input                          *string
	version                                       *int
	employment, role, eligibility, location, grad *string
	reasons, skills, breakdown, modelOutput       *string
	comp                                          *float64
	duration, class, modelUsed, modelKey          *string
	score                                         *int
	assessedAt                                    *time.Time
}

func (a *assessmentRow) targets() []any {
	return []any{&a.listingID, &a.descHash, &a.key, &a.input, &a.version, &a.employment, &a.role, &a.eligibility,
		&a.reasons, &a.location, &a.grad, &a.comp, &a.duration, &a.skills, &a.score,
		&a.breakdown, &a.class, &a.assessedAt, &a.modelUsed, &a.modelKey, &a.modelOutput}
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// build turns the row into an Assessment, or nil when the listing has none.
func (a *assessmentRow) build(userID uuid.UUID) (*domain.Assessment, error) {
	if a.listingID == nil {
		return nil, nil
	}
	out := &domain.Assessment{
		ListingID: *a.listingID, UserID: userID, DescriptionHash: a.descHash, AssessmentKey: str(a.key), InputHash: str(a.input),
		EmploymentClass: str(a.employment), RoleClass: str(a.role), Eligibility: str(a.eligibility), LocationFit: str(a.location), GradYearFit: str(a.grad),
		CompensationMonthlyINR: a.comp, Duration: a.duration, ValueClass: str(a.class), ModelUsed: a.modelUsed, ModelKey: a.modelKey,
		EligibilityReasons: []domain.Reason{}, SkillsMatched: []domain.SkillMatch{}, ValueBreakdown: []domain.ValueComponent{},
	}
	if a.version != nil {
		out.Version = *a.version
	}
	if a.score != nil {
		out.ValueScore = *a.score
	}
	if a.assessedAt != nil {
		out.AssessedAt = *a.assessedAt
	}
	for _, f := range []struct {
		raw *string
		dst any
	}{{a.reasons, &out.EligibilityReasons}, {a.skills, &out.SkillsMatched}, {a.breakdown, &out.ValueBreakdown}} {
		if f.raw != nil && *f.raw != "" {
			if err := json.Unmarshal([]byte(*f.raw), f.dst); err != nil {
				return nil, fmt.Errorf("read assessment json: %w", err)
			}
		}
	}
	if a.modelOutput != nil {
		out.ModelOutput = json.RawMessage(*a.modelOutput)
	}
	if out.CompensationMonthlyINR != nil {
		for _, c := range out.ValueBreakdown {
			if c.Name == "compensation" {
				out.CompensationQuote = c.Quote
			}
		}
	}
	return out, nil
}

// GetAssessment returns a listing's current assessment, or nil when it has none.
func (r *OpportunityRepository) GetAssessment(ctx context.Context, userID, listingID uuid.UUID) (*domain.Assessment, error) {
	var row assessmentRow
	err := r.pool.QueryRow(ctx, `SELECT `+assessmentCols+` FROM listing_assessments a WHERE a.user_id = $1 AND a.listing_id = $2`, userID, listingID).Scan(row.targets()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assessment: %w", err)
	}
	return row.build(userID)
}

func jsonText(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Evidence claims written by an assessment. They are replaced, not appended, each time a listing is re-assessed.
var assessmentClaims = []string{"compensation_monthly_inr", "eligibility_blocker", "eligibility_fit"}

// SaveAssessment stores a listing's assessment (one row per listing; a new one replaces the old) and the evidence
// behind its pay and eligibility conclusions: each with the exact words and the listing's link.
func (r *OpportunityRepository) SaveAssessment(ctx context.Context, a *domain.Assessment, sourceURL string) error {
	reasons, err := jsonText(a.EligibilityReasons)
	if err != nil {
		return err
	}
	skills, err := jsonText(a.SkillsMatched)
	if err != nil {
		return err
	}
	breakdown, err := jsonText(a.ValueBreakdown)
	if err != nil {
		return err
	}
	var modelOutput *string
	if len(a.ModelOutput) > 0 {
		s := string(a.ModelOutput)
		modelOutput = &s
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO listing_assessments (listing_id, user_id, description_hash, assessment_key, input_hash, assessor_version, employment_class, role_class,
			eligibility, eligibility_reasons, location_fit, grad_year_fit, compensation_monthly_inr, duration, skills_matched, value_score, value_breakdown,
			value_class, assessed_at, model_used, model_key, model_output)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13::numeric, $14, $15::jsonb, $16, $17::jsonb, $18, $19, $20, $21, $22::jsonb)
		ON CONFLICT (listing_id) DO UPDATE SET
			description_hash = EXCLUDED.description_hash, assessment_key = EXCLUDED.assessment_key, input_hash = EXCLUDED.input_hash,
			assessor_version = EXCLUDED.assessor_version, employment_class = EXCLUDED.employment_class, role_class = EXCLUDED.role_class,
			eligibility = EXCLUDED.eligibility, eligibility_reasons = EXCLUDED.eligibility_reasons, location_fit = EXCLUDED.location_fit,
			grad_year_fit = EXCLUDED.grad_year_fit, compensation_monthly_inr = EXCLUDED.compensation_monthly_inr, duration = EXCLUDED.duration,
			skills_matched = EXCLUDED.skills_matched, value_score = EXCLUDED.value_score, value_breakdown = EXCLUDED.value_breakdown,
			value_class = EXCLUDED.value_class, assessed_at = EXCLUDED.assessed_at, model_used = EXCLUDED.model_used,
			model_key = EXCLUDED.model_key, model_output = EXCLUDED.model_output`,
		a.ListingID, a.UserID, a.DescriptionHash, a.AssessmentKey, a.InputHash, a.Version, a.EmploymentClass, a.RoleClass,
		a.Eligibility, reasons, a.LocationFit, a.GradYearFit, a.CompensationMonthlyINR, a.Duration, skills, a.ValueScore, breakdown,
		a.ValueClass, a.AssessedAt, a.ModelUsed, a.ModelKey, modelOutput)
	if err != nil {
		return fmt.Errorf("save assessment: %w", err)
	}

	// Evidence: replace what an earlier assessment wrote.
	in := make([]string, len(assessmentClaims))
	args := []any{a.UserID, a.ListingID}
	for i, c := range assessmentClaims {
		in[i] = "$" + strconv.Itoa(i+3)
		args = append(args, c)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM listing_evidence WHERE user_id = $1 AND listing_id = $2 AND claim IN (`+strings.Join(in, ", ")+`)`, args...); err != nil {
		return fmt.Errorf("replace assessment evidence: %w", err)
	}
	add := func(claim, value, quote string) error {
		return r.AddEvidence(ctx, &domain.ListingEvidence{UserID: a.UserID, ListingID: a.ListingID, Claim: claim, Value: value, Quote: quote, SourceURL: sourceURL, CapturedAt: a.AssessedAt})
	}
	if a.CompensationMonthlyINR != nil && a.CompensationQuote != "" {
		if err := add("compensation_monthly_inr", strconv.FormatFloat(*a.CompensationMonthlyINR, 'f', 0, 64), a.CompensationQuote); err != nil {
			return err
		}
	}
	claim := "eligibility_fit"
	if a.Eligibility == domain.EligibilityIneligible {
		claim = "eligibility_blocker"
	}
	for _, rs := range a.EligibilityReasons {
		if rs.Quote == "" {
			continue
		}
		if err := add(claim, rs.Reason, rs.Quote); err != nil {
			return err
		}
	}
	return nil
}

// ListAssessed returns the owner's listings with their assessments, best value first (listings not yet assessed
// last, newest first). It filters by stage, value class and eligibility.
func (r *OpportunityRepository) ListAssessed(ctx context.Context, userID uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	q := `SELECT ` + prefixed("j.", listingCols) + `, ` + assessmentCols + `
		FROM job_discoveries j LEFT JOIN listing_assessments a ON a.listing_id = j.id WHERE j.user_id = $1`
	args := []any{userID}
	in := func(col string, vals []string) string {
		ph := make([]string, len(vals))
		for i, v := range vals {
			args = append(args, v)
			ph[i] = "$" + strconv.Itoa(len(args))
		}
		return col + " IN (" + strings.Join(ph, ", ") + ")"
	}
	if len(f.Stages) > 0 {
		q += " AND " + in("j.stage", f.Stages)
	}
	if len(f.Tiers) > 0 {
		q += " AND " + in("j.tier", f.Tiers)
	}
	if len(f.Classes) > 0 {
		q += " AND " + in("a.value_class", f.Classes)
	}
	if len(f.Eligibility) > 0 {
		clause := in("a.eligibility", f.Eligibility)
		for _, e := range f.Eligibility {
			if e == domain.EligibilityUnknown {
				clause = "(" + clause + " OR a.listing_id IS NULL)"
				break
			}
		}
		q += " AND " + clause
	}
	q += fmt.Sprintf(" ORDER BY a.value_score DESC NULLS LAST, j.created_at DESC LIMIT %d", limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list assessed listings: %w", err)
	}
	defer rows.Close()
	var out []*domain.Listing
	for rows.Next() {
		l := &domain.Listing{UserID: userID}
		var row assessmentRow
		if err := rows.Scan(append(scanTargets(l), row.targets()...)...); err != nil {
			return nil, fmt.Errorf("scan assessed listing: %w", err)
		}
		if l.Assessment, err = row.build(userID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
