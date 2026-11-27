package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// PeopleRepository reads and writes person records: canonical_entities rows of kind person, with their outreach
// fields. Research adds to these same rows; there is no second people store.
type PeopleRepository struct {
	pool database.Querier
}

// NewPeopleRepository constructs a PeopleRepository.
func NewPeopleRepository(pool database.Querier) *PeopleRepository {
	return &PeopleRepository{pool: pool}
}

const personCols = `id, name, profile_url, metadata, graduation_year, graduation_year_evidence_id, do_not_contact, contact_state, last_verified_at, created_at, updated_at,
	campus, vit_affiliation, branch, degree, graduation_date, graduation_eligibility, acm_vit_affiliation, acm_role, acm_tenure, acm_source, acm_evidence_excerpt,
	normalized_title, professional_function, seniority, classification_confidence, current_company_id, contactability_status, preferred_channel,
	public_contact_route, public_business_email, priority_tier, priority_score, priority_rationale`

// scanPerson reads a person. The classification fields come from their columns; metadata keeps only what a source
// said about the person (company, title, club, country, role family, provenance).
func scanPerson(row pgx.Row) (*domain.Person, error) {
	var p domain.Person
	var meta []byte
	var state string
	if err := row.Scan(&p.ID, &p.Name, &p.ProfileURL, &meta, &p.GraduationYear, &p.GraduationYearEvidenceID, &p.DoNotContact, &state, &p.LastVerifiedAt, &p.CreatedAt, &p.UpdatedAt,
		&p.Campus, &p.VITAffiliation, &p.Branch, &p.Degree, &p.GraduationDate, &p.GraduationEligibility, &p.ACMAffiliation, &p.ACMRole, &p.ACMTenure, &p.ACMSource, &p.ACMEvidenceExcerpt,
		&p.NormalizedTitle, &p.ProfessionalFunction, &p.Seniority, &p.ClassificationConfidence, &p.CurrentCompanyID, &p.ContactabilityStatus, &p.PreferredChannel,
		&p.PublicContactRoute, &p.PublicBusinessEmail, &p.PriorityTier, &p.PriorityScore, &p.PriorityRationale); err != nil {
		return nil, err
	}
	p.ContactState = domain.ContactState(state)
	p.Metadata = map[string]any{}
	if err := unmarshalMeta(meta, &p.Metadata); err != nil {
		return nil, err
	}
	str := func(k string) string { s, _ := p.Metadata[k].(string); return s }
	p.Company, p.Title, p.Club, p.Country = str("company"), str("title"), str("club"), str("country")
	p.RoleFamily = str("role_family")
	p.RoleFamilyInferred, _ = p.Metadata["role_family_inferred"].(bool)
	p.GraduationYearSource = str("graduation_year_source")
	return &p, nil
}

// typedCol is one typed column of a person with the SQL type of its parameter, the value a new row gets when none
// is given, and the value (nil when the caller gave none).
type typedCol struct {
	name, typ, def string
	val            any
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// personTypedCols lists the columns UpsertPerson and ImportAlumni write from a person. A field left empty is nil:
// a new row takes the default, and an existing row keeps what it has.
func personTypedCols(p *domain.Person) []typedCol {
	var conf, score, gradDate, company any
	if p.ClassificationConfidence != 0 {
		conf = p.ClassificationConfidence
	}
	if p.PriorityScore != 0 {
		score = p.PriorityScore
	}
	if p.GraduationDate != nil && !p.GraduationDate.IsZero() {
		gradDate = *p.GraduationDate
	}
	if p.CurrentCompanyID != nil {
		company = *p.CurrentCompanyID
	}
	return []typedCol{
		{"campus", "text", "'unknown'", nz(p.Campus)},
		{"vit_affiliation", "text", "'unconfirmed'", nz(p.VITAffiliation)},
		{"branch", "text", "''", nz(p.Branch)},
		{"degree", "text", "''", nz(p.Degree)},
		{"graduation_date", "date", "NULL", gradDate},
		{"graduation_eligibility", "text", "'unknown'", nz(p.GraduationEligibility)},
		{"acm_vit_affiliation", "text", "'unknown'", nz(p.ACMAffiliation)},
		{"acm_role", "text", "''", nz(p.ACMRole)},
		{"acm_tenure", "text", "''", nz(p.ACMTenure)},
		{"acm_source", "text", "''", nz(p.ACMSource)},
		{"acm_evidence_excerpt", "text", "''", nz(p.ACMEvidenceExcerpt)},
		{"normalized_title", "text", "''", nz(p.NormalizedTitle)},
		{"professional_function", "text", "'unknown'", nz(p.ProfessionalFunction)},
		{"seniority", "text", "'unknown'", nz(p.Seniority)},
		{"classification_confidence", "int", "50", conf},
		{"current_company_id", "uuid", "NULL", company},
		{"contactability_status", "text", "'unknown'", nz(p.ContactabilityStatus)},
		{"preferred_channel", "text", "'linkedin'", nz(p.PreferredChannel)},
		{"public_contact_route", "text", "''", nz(p.PublicContactRoute)},
		{"public_business_email", "text", "''", nz(p.PublicBusinessEmail)},
		{"priority_tier", "text", "'REVIEW'", nz(p.PriorityTier)},
		{"priority_score", "int", "0", score},
		{"priority_rationale", "text", "''", nz(p.PriorityRationale)},
	}
}

// typedSQL returns the column list and values of an INSERT and the SET clauses of its ON CONFLICT update for the
// typed columns, numbering parameters from first. When guard is not empty the update changes a column only when the
// guard is true; otherwise a value the caller gave replaces the stored one and an empty one leaves it.
func typedSQL(cols []typedCol, first int, guard string) (names, values, sets string, args []any) {
	var n, v, u []string
	for i, c := range cols {
		ph := fmt.Sprintf("$%d::%s", first+i, c.typ)
		n = append(n, c.name)
		v = append(v, fmt.Sprintf("COALESCE(%s, %s)", ph, c.def))
		set := fmt.Sprintf("COALESCE(%s, canonical_entities.%s)", ph, c.name)
		if guard != "" {
			set = fmt.Sprintf("CASE WHEN %s THEN %s ELSE canonical_entities.%s END", guard, set, c.name)
		}
		u = append(u, c.name+" = "+set)
		args = append(args, c.val)
	}
	return strings.Join(n, ", "), strings.Join(v, ", "), strings.Join(u, ",\n\t\t\t\t"), args
}

// prioritise fills the tier, score and rationale of a person who has none, from the other fields.
func prioritise(p *domain.Person, payPotential string) {
	if p.PriorityTier != "" {
		return
	}
	p.PriorityTier, p.PriorityScore, p.PriorityRationale = domain.EvaluatePriorityTier(p.ACMAffiliation, payPotential, p.Company,
		p.VITAffiliation == domain.AffiliationConfirmedVellore, p.GraduationEligibility == domain.CohortEligible, p.GraduationEligibility == domain.CohortBorderline2022)
}

// ImportResult says what an import did.
type ImportResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// selfReported is the source of a person made from the Alumni tracker: what the person or the owner typed, with no
// evidence behind it. Such a person has no confirmed campus or ACM role, so lands in REVIEW, never P1 to P4.
const selfReported = "linkedin_self_reported"

// ImportAlumni writes the Alumni tracker's rows as person records, keyed on the canonical profile URL, so running it
// again changes nothing it already wrote. It fills only what a row has. A graduation year backed by evidence is never
// replaced by a tracker's batch; do-not-contact is only ever turned on; a contact state the owner moved is kept; a
// person a better source wrote (source is not linkedin_self_reported) keeps its classification columns.
func (r *PeopleRepository) ImportAlumni(ctx context.Context, userID uuid.UUID, rows []domain.AlumniImportRow) (ImportResult, error) {
	var res ImportResult
	for _, row := range rows {
		link := domain.CanonicalLinkedInURL(row.ProfileURL)
		if link == "" || row.Name == "" {
			res.Skipped++
			continue
		}
		meta := map[string]any{"source": selfReported}
		for k, v := range map[string]string{"company": row.Company, "title": row.Role, "club": row.Club} {
			if v != "" {
				meta[k] = v
			}
		}

		norm, fn, fam, sen, conf := domain.ClassifyRole(row.Role)
		if fam != "" {
			meta["role_family"], meta["role_family_inferred"] = fam, true
		}
		p := &domain.Person{
			Company: row.Company, NormalizedTitle: norm, ProfessionalFunction: fn, Seniority: sen, ClassificationConfidence: conf,
			ContactabilityStatus: "profile_only", PreferredChannel: "linkedin",
		}
		// The tracker has no school column; the club text is all there is to read, and it rarely names a campus.
		p.Campus, p.VITAffiliation = domain.ClassifyVITCampus(row.Club)
		// No source and no excerpt: a self-reported ACM mention is "likely" at most, never "confirmed".
		p.ACMAffiliation = domain.ResolveACMAffiliation(row.Club, "", "")

		var year any
		var gradYear *int
		if row.GraduationYear != 0 {
			year = row.GraduationYear
			meta["graduation_year_source"] = "tracker_batch"
			gradYear = &row.GraduationYear
		}
		// A year backed by evidence is not replaced by the tracker's batch, so eligibility follows the stored year.
		var storedYear *int
		var hasEvidence bool
		err := r.pool.QueryRow(ctx, `SELECT graduation_year, graduation_year_evidence_id IS NOT NULL FROM canonical_entities
			WHERE user_id = $1 AND kind = 'person' AND profile_url = $2`, userID, link).Scan(&storedYear, &hasEvidence)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return res, fmt.Errorf("import alumni %q: %w", row.Name, err)
		}
		if hasEvidence {
			gradYear = storedYear
		}
		p.GraduationYear = gradYear
		p.GraduationEligibility = domain.EvaluateGraduationEligibility(gradYear, nil, domain.DefaultCutoffDate)
		prioritise(p, "")

		mj, err := marshalMeta(meta)
		if err != nil {
			return res, err
		}
		key := "li:" + link
		// A person made earlier without a link (same key) takes the link instead of colliding with it.
		if _, err := r.pool.Exec(ctx, `UPDATE canonical_entities SET profile_url = $3 WHERE user_id = $1 AND kind = 'person' AND canonical_key = $2 AND profile_url IS NULL`,
			userID, key, link); err != nil {
			return res, fmt.Errorf("import alumni: %w", err)
		}
		names, values, sets, targs := typedSQL(personTypedCols(p), 9, "canonical_entities.metadata->>'source' = '"+selfReported+"' AND canonical_entities.metadata->'verification' IS NULL")
		args := append([]any{userID, key, row.Name, string(mj), link, year, row.DoNotContact, string(row.ContactState)}, targs...)
		var inserted bool
		err = r.pool.QueryRow(ctx, `
			INSERT INTO canonical_entities (user_id, kind, canonical_key, name, metadata, profile_url, graduation_year, do_not_contact, contact_state, `+names+`)
			VALUES ($1, 'person', $2, $3, $4::jsonb, $5, $6, $7, $8, `+values+`)
			ON CONFLICT (user_id, profile_url) WHERE profile_url IS NOT NULL DO UPDATE SET
				name = EXCLUDED.name,
				metadata = canonical_entities.metadata || (EXCLUDED.metadata - 'source'),
				graduation_year = CASE WHEN canonical_entities.graduation_year_evidence_id IS NOT NULL THEN canonical_entities.graduation_year
				                       ELSE COALESCE(EXCLUDED.graduation_year, canonical_entities.graduation_year) END,
				do_not_contact = canonical_entities.do_not_contact OR EXCLUDED.do_not_contact,
				contact_state = CASE WHEN EXCLUDED.do_not_contact THEN 'opted_out'
				                     WHEN canonical_entities.contact_state = 'not_contacted' THEN EXCLUDED.contact_state
				                     ELSE canonical_entities.contact_state END,
				`+sets+`,
				updated_at = CASE WHEN canonical_entities.name IS DISTINCT FROM EXCLUDED.name
				                   OR canonical_entities.metadata <> (canonical_entities.metadata || (EXCLUDED.metadata - 'source'))
				                   OR canonical_entities.graduation_year IS DISTINCT FROM COALESCE(EXCLUDED.graduation_year, canonical_entities.graduation_year)
				                   OR (EXCLUDED.do_not_contact AND NOT canonical_entities.do_not_contact)
				                   OR (canonical_entities.contact_state = 'not_contacted' AND EXCLUDED.contact_state <> 'not_contacted')
				                 THEN NOW() ELSE canonical_entities.updated_at END
			RETURNING (xmax = 0)`, args...,
		).Scan(&inserted)
		if err != nil {
			return res, fmt.Errorf("import alumni %q: %w", row.Name, err)
		}
		if inserted {
			res.Created++
		} else {
			res.Updated++
		}
	}
	return res, nil
}

// PersonFilter narrows the people list. A nil or empty field filters nothing.
type PersonFilter struct {
	GradYearMax         *int   // graduated in this year or earlier; an unknown year never matches
	RoleFamily          string // tech or operations
	Company             string // part of the company name, any case
	Country             string
	Campus              string
	ACMAffiliation      string
	PriorityTier        string
	Seniority           string
	GradEligibility     string
	CompanyPayPotential string
	// ProfessionalFunction, ContactabilityStatus and MinClassificationConfidence read the typed columns; every
	// filter set is ANDed with the others.
	ProfessionalFunction        string
	ContactabilityStatus        string
	MinClassificationConfidence *int
	Search                      string
	DoNotContact                *bool
	SortByPriority              bool
	Limit                       int
}

// List returns the owner's people, newest first.
func (r *PeopleRepository) List(ctx context.Context, userID uuid.UUID, f PersonFilter) ([]*domain.Person, error) {
	q := `SELECT ` + personCols + ` FROM canonical_entities WHERE user_id = $1 AND kind = 'person'`
	args := []any{userID}
	add := func(cond string, v any) {
		args = append(args, v)
		q += " AND " + strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args)))
	}
	if f.GradYearMax != nil {
		add("graduation_year IS NOT NULL AND graduation_year <= ?", *f.GradYearMax)
	}
	if f.RoleFamily != "" {
		add("metadata->>'role_family' = ?", f.RoleFamily)
	}
	if f.Company != "" {
		add("position(lower(?::text) in lower(coalesce(metadata->>'company', ''))) > 0", f.Company)
	}
	if f.Country != "" {
		add("lower(coalesce(metadata->>'country', '')) = lower(?::text)", f.Country)
	}
	if f.Campus != "" {
		add("lower(campus) = lower(?::text)", f.Campus)
	}
	if f.ACMAffiliation != "" {
		add("lower(acm_vit_affiliation) = lower(?::text)", f.ACMAffiliation)
	}
	if f.PriorityTier != "" {
		add("priority_tier = ?", f.PriorityTier)
	}
	if f.Seniority != "" {
		add("seniority = ?", f.Seniority)
	}
	if f.GradEligibility != "" {
		add("graduation_eligibility = ?", f.GradEligibility)
	}
	if f.ProfessionalFunction != "" {
		add("professional_function = ?", f.ProfessionalFunction)
	}
	if f.ContactabilityStatus != "" {
		add("contactability_status = ?", f.ContactabilityStatus)
	}
	if f.MinClassificationConfidence != nil {
		add("classification_confidence >= ?", *f.MinClassificationConfidence)
	}
	if f.Search != "" {
		add("(position(lower(?::text) in lower(name)) > 0 OR position(lower(?::text) in lower(coalesce(metadata->>'title', ''))) > 0 OR position(lower(?::text) in lower(coalesce(metadata->>'company', ''))) > 0)", f.Search)
	}
	if f.DoNotContact != nil {
		add("do_not_contact = ?", *f.DoNotContact)
	}
	order := ` ORDER BY created_at DESC, name`
	if f.SortByPriority {
		order = ` ORDER BY priority_score DESC, created_at DESC, name`
	}
	args = append(args, f.Limit)
	rows, err := r.pool.Query(ctx, q+order+fmt.Sprintf(` LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	defer rows.Close()
	out := []*domain.Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get returns one person; domain.ErrNotFound when the id is not the owner's person.
func (r *PeopleRepository) Get(ctx context.Context, userID, id uuid.UUID) (*domain.Person, error) {
	p, err := scanPerson(r.pool.QueryRow(ctx, `SELECT `+personCols+` FROM canonical_entities WHERE id = $1 AND user_id = $2 AND kind = 'person'`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get person: %w", err)
	}
	return p, nil
}

// Update sets the contact state and/or do-not-contact flag the owner chose. Turning do-not-contact on moves the
// contact state to opted_out unless the owner gave a state in the same call.
func (r *PeopleRepository) Update(ctx context.Context, userID, id uuid.UUID, state *domain.ContactState, dnc *bool) (*domain.Person, error) {
	var st any
	if state != nil {
		st = string(*state)
	}
	p, err := scanPerson(r.pool.QueryRow(ctx, `
		UPDATE canonical_entities SET
			contact_state = COALESCE($3, CASE WHEN $4::boolean IS TRUE THEN 'opted_out' ELSE contact_state END),
			do_not_contact = COALESCE($4, do_not_contact), updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND kind = 'person'
		RETURNING `+personCols, id, userID, st, dnc))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update person: %w", err)
	}
	return p, nil
}

// UpdatePriority stores a person's computed tier, score and rationale.
func (r *PeopleRepository) UpdatePriority(ctx context.Context, userID, id uuid.UUID, tier string, score int, rationale string) error {
	_, err := r.pool.Exec(ctx, `UPDATE canonical_entities SET priority_tier = $3, priority_score = $4, priority_rationale = $5, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND kind = 'person'`, id, userID, tier, score, rationale)
	if err != nil {
		return fmt.Errorf("update priority: %w", err)
	}
	return nil
}

// EvidenceState is what the quote check needs to know about one person: who they are and which evidence (page and
// quote) was last checked for them, so the same page is not fetched again.
type EvidenceState struct {
	ID      uuid.UUID
	Checked string
}

// EvidenceStateByProfile returns the person with this profile link, or ErrNotFound.
func (r *PeopleRepository) EvidenceStateByProfile(ctx context.Context, userID uuid.UUID, link string) (EvidenceState, error) {
	var st EvidenceState
	err := r.pool.QueryRow(ctx, `SELECT id, COALESCE(metadata->'evidence_check'->>'key', '') FROM canonical_entities
		WHERE user_id = $1 AND kind = 'person' AND profile_url = $2`, userID, domain.CanonicalLinkedInURL(link)).Scan(&st.ID, &st.Checked)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, domain.ErrNotFound
	}
	if err != nil {
		return st, fmt.Errorf("evidence state: %w", err)
	}
	return st, nil
}

// MarkEvidenceChecked records that the evidence key (page and quote) was checked, with the result.
func (r *PeopleRepository) MarkEvidenceChecked(ctx context.Context, userID, id uuid.UUID, key, result string) error {
	b, _ := json.Marshal(map[string]string{"key": key, "result": result, "at": time.Now().UTC().Format(time.RFC3339)})
	if _, err := r.pool.Exec(ctx, `UPDATE canonical_entities SET metadata = jsonb_set(metadata, '{evidence_check}', $3::jsonb)
		WHERE id = $1 AND user_id = $2 AND kind = 'person'`, id, userID, string(b)); err != nil {
		return fmt.Errorf("mark evidence checked: %w", err)
	}
	return nil
}

// ApplyVerification stores the owner's evidence-backed claims on a person, works out the graduation eligibility and
// the priority tier again from the stored fields, and records each claim's provenance in metadata. It works on a
// do-not-contact person too (the data stays accurate); the queue still skips them. domain.ErrNotFound when the id
// is not the owner's person.
func (r *PeopleRepository) ApplyVerification(ctx context.Context, userID, id uuid.UUID, v *domain.VerifiedFields) (*domain.Person, error) {
	prov, err := json.Marshal(map[string]any{"verification": v.Provenance})
	if err != nil {
		return nil, fmt.Errorf("apply verification: %w", err)
	}
	var gradDate any
	if v.GraduationDate != nil {
		gradDate = *v.GraduationDate
	}
	p, err := scanPerson(r.pool.QueryRow(ctx, `
		UPDATE canonical_entities SET
			campus = COALESCE($3::text, campus), vit_affiliation = COALESCE($4::text, vit_affiliation),
			graduation_year = COALESCE($5::int, graduation_year), graduation_date = COALESCE($6::date, graduation_date),
			acm_vit_affiliation = COALESCE($7::text, acm_vit_affiliation), acm_role = COALESCE($8::text, acm_role),
			acm_tenure = COALESCE($9::text, acm_tenure), acm_source = COALESCE($10::text, acm_source),
			acm_evidence_excerpt = COALESCE($11::text, acm_evidence_excerpt),
			public_contact_route = COALESCE($12::text, public_contact_route), public_business_email = COALESCE($13::text, public_business_email),
			metadata = jsonb_set(metadata, '{verification}', COALESCE(metadata->'verification', '{}'::jsonb) || ($14::jsonb->'verification')),
			last_verified_at = $15, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND kind = 'person'
		RETURNING `+personCols,
		id, userID, v.Campus, v.VITAffiliation, v.GraduationYear, gradDate, v.ACMAffiliation, v.ACMRole, v.ACMTenure, v.ACMSource,
		v.ACMExcerpt, v.ContactRoute, v.BusinessEmail, prov, time.Now().UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("apply verification: %w", err)
	}
	p.GraduationEligibility = domain.EvaluateGraduationEligibility(p.GraduationYear, p.GraduationDate, domain.DefaultCutoffDate)
	p.PriorityTier = ""
	pay, _ := p.Metadata["pay_potential"].(string)
	if pay == "" {
		pay = domain.PayPotentialLowUnknown
	}
	prioritise(p, pay)
	if _, err := r.pool.Exec(ctx, `UPDATE canonical_entities SET graduation_eligibility = $3, priority_tier = $4, priority_score = $5, priority_rationale = $6
		WHERE id = $1 AND user_id = $2`, p.ID, userID, p.GraduationEligibility, p.PriorityTier, p.PriorityScore, p.PriorityRationale); err != nil {
		return nil, fmt.Errorf("apply verification: %w", err)
	}
	return p, nil
}

// MarkDrafted sets contact_state to drafted, unless the person is do-not-contact (or already further along); it
// reports whether it did.
func (r *PeopleRepository) MarkDrafted(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE canonical_entities SET contact_state = 'drafted', updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND kind = 'person' AND do_not_contact = false AND contact_state IN ('not_contacted', 'drafted', 'approved')`, id, userID)
	if err != nil {
		return false, fmt.Errorf("mark drafted: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// DecideDraft records the owner's decision on a draft: approved, or (when rejected) back to not_contacted. It
// touches only a person who is not do-not-contact and whose draft is still open. Nothing is sent.
func (r *PeopleRepository) DecideDraft(ctx context.Context, userID, id uuid.UUID, approved bool) error {
	to := "not_contacted"
	if approved {
		to = "approved"
	}
	_, err := r.pool.Exec(ctx, `UPDATE canonical_entities SET contact_state = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND kind = 'person' AND do_not_contact = false AND contact_state = 'drafted'`, id, userID, to)
	if err != nil {
		return fmt.Errorf("decide draft: %w", err)
	}
	return nil
}

// DoNotContactProfiles returns the canonical profile URLs (domain.CanonicalLinkedInURL) of every person the owner
// must not be proposed (flagged, or opted out).
func (r *PeopleRepository) DoNotContactProfiles(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT profile_url FROM canonical_entities
		WHERE user_id = $1 AND kind = 'person' AND profile_url IS NOT NULL AND (do_not_contact OR contact_state = 'opted_out')`, userID)
	if err != nil {
		return nil, fmt.Errorf("do not contact: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out[domain.CanonicalLinkedInURL(u)] = true
	}
	return out, rows.Err()
}

// SetGraduationYear sets the year (and the evidence that states it) of the person with this profile URL, and
// stamps last_verified_at. It reports whether such a person exists. The eligibility follows the year (unless an
// exact graduation date is stored), and a tier that depended on it is worked out again.
func (r *PeopleRepository) SetGraduationYear(ctx context.Context, userID uuid.UUID, profileURL string, year int, evidenceID *uuid.UUID) (bool, error) {
	link := domain.CanonicalLinkedInURL(profileURL)
	if link == "" {
		return false, nil
	}
	p, err := scanPerson(r.pool.QueryRow(ctx, `
		UPDATE canonical_entities SET graduation_year = $3, graduation_year_evidence_id = $4, last_verified_at = $5,
			metadata = metadata || '{"graduation_year_source":"evidence"}'::jsonb, updated_at = NOW()
		WHERE user_id = $1 AND kind = 'person' AND profile_url = $2
		RETURNING `+personCols,
		userID, link, year, evidenceID, time.Now().UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("set graduation year: %w", err)
	}
	elig := domain.EvaluateGraduationEligibility(p.GraduationYear, p.GraduationDate, domain.DefaultCutoffDate)
	if elig == p.GraduationEligibility {
		return true, nil
	}
	p.GraduationEligibility, p.PriorityTier = elig, ""
	pay, _ := p.Metadata["pay_potential"].(string)
	prioritise(p, pay)
	if _, err := r.pool.Exec(ctx, `UPDATE canonical_entities SET graduation_eligibility = $3, priority_tier = $4, priority_score = $5, priority_rationale = $6
		WHERE id = $1 AND user_id = $2`, p.ID, userID, p.GraduationEligibility, p.PriorityTier, p.PriorityScore, p.PriorityRationale); err != nil {
		return false, fmt.Errorf("set graduation year: %w", err)
	}
	return true, nil
}

// UpsertPerson creates or updates a person record. The qualification attributes go to their typed columns;
// metadata keeps the source's own words (company, title, club, country, role family) and provenance. The person is
// keyed on the canonical LinkedIn URL, never on the name. An ACM claim is resolved first: "confirmed" is stored only
// with a source URL and an excerpt. When a known person's company or title changes, the old job is kept in
// person_employments and the new one is the current job.
func (r *PeopleRepository) UpsertPerson(ctx context.Context, userID uuid.UUID, p *domain.Person) (*domain.Person, error) {
	if p.ProfileURL == nil || p.Name == "" {
		return nil, fmt.Errorf("%w: name and profile_url are required", domain.ErrValidation)
	}
	link := domain.CanonicalLinkedInURL(*p.ProfileURL)
	if link == "" {
		return nil, fmt.Errorf("%w: name and a LinkedIn profile_url are required", domain.ErrValidation)
	}
	key := "li:" + link
	p.ACMAffiliation = domain.ResolveACMAffiliation(p.ACMAffiliation, p.ACMSource, p.ACMEvidenceExcerpt)
	if p.NormalizedTitle == "" && p.Title != "" {
		var fam string
		p.NormalizedTitle, p.ProfessionalFunction, fam, p.Seniority, p.ClassificationConfidence = domain.ClassifyRole(p.Title)
		if p.RoleFamily == "" {
			p.RoleFamily = fam
		}
	}
	if e := domain.EvaluateGraduationEligibility(p.GraduationYear, p.GraduationDate, domain.DefaultCutoffDate); p.GraduationEligibility == "" && e != domain.CohortUnknown {
		p.GraduationEligibility = e // no year or date says nothing, and must not wipe what is stored
	}
	pay, _ := p.Metadata["pay_potential"].(string)
	prioritise(p, pay)

	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	meta := p.Metadata
	for k, v := range map[string]string{"company": p.Company, "title": p.Title, "club": p.Club, "country": p.Country, "role_family": p.RoleFamily} {
		if v != "" {
			meta[k] = v
		}
	}
	mj, err := marshalMeta(meta)
	if err != nil {
		return nil, err
	}

	state := string(p.ContactState)
	if !p.ContactState.Valid() {
		state = string(domain.ContactNotContacted)
	}

	// What the stored person did for a living before this write, to keep it if the job changed.
	var oldCompany, oldTitle string
	err = r.pool.QueryRow(ctx, `SELECT coalesce(metadata->>'company', ''), coalesce(metadata->>'title', '') FROM canonical_entities
		WHERE user_id = $1 AND kind = 'person' AND profile_url = $2`, userID, link).Scan(&oldCompany, &oldTitle)
	existed := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("upsert person: %w", err)
	}

	names, values, sets, targs := typedSQL(personTypedCols(p), 10, "")
	args := append([]any{userID, key, p.Name, string(mj), link, p.GraduationYear, p.DoNotContact, state, p.LastVerifiedAt}, targs...)
	row := r.pool.QueryRow(ctx, `
		INSERT INTO canonical_entities (user_id, kind, canonical_key, name, metadata, profile_url, graduation_year, do_not_contact, contact_state, last_verified_at, `+names+`)
		VALUES ($1, 'person', $2, $3, $4::jsonb, $5, $6, $7, $8, $9, `+values+`)
		ON CONFLICT (user_id, profile_url) WHERE profile_url IS NOT NULL DO UPDATE SET
			name = EXCLUDED.name,
			metadata = canonical_entities.metadata || EXCLUDED.metadata,
			graduation_year = COALESCE(EXCLUDED.graduation_year, canonical_entities.graduation_year),
			do_not_contact = canonical_entities.do_not_contact OR EXCLUDED.do_not_contact,
			contact_state = CASE WHEN EXCLUDED.do_not_contact THEN 'opted_out'
			                     WHEN canonical_entities.contact_state = 'not_contacted' THEN EXCLUDED.contact_state
			                     ELSE canonical_entities.contact_state END,
			last_verified_at = COALESCE(EXCLUDED.last_verified_at, canonical_entities.last_verified_at),
			`+sets+`,
			updated_at = NOW()
		RETURNING `+personCols, args...)
	out, err := scanPerson(row)
	if err != nil {
		return nil, err
	}

	changed := (p.Company != "" && oldCompany != "" && p.Company != oldCompany) || (p.Title != "" && oldTitle != "" && p.Title != oldTitle)
	if existed && changed {
		if err := r.recordJobChange(ctx, userID, out.ID, oldCompany, oldTitle, p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// recordJobChange keeps the job a person left in person_employments (not current) and adds the new one as the
// current job. A job without both an employer and a title cannot be stored, so it is left out rather than guessed.
func (r *PeopleRepository) recordJobChange(ctx context.Context, userID, personID uuid.UUID, oldCompany, oldTitle string, now *domain.Person) error {
	if oldCompany != "" && oldTitle != "" {
		if _, err := r.pool.Exec(ctx, `UPDATE person_employments pe SET is_current = false
			WHERE pe.user_id = $1 AND pe.person_id = $2 AND pe.is_current AND pe.employer_name = $3 AND pe.title = $4
			  AND NOT EXISTS (SELECT 1 FROM person_employments x WHERE x.person_id = pe.person_id AND x.employer_name = pe.employer_name AND x.title = pe.title AND NOT x.is_current)`,
			userID, personID, oldCompany, oldTitle); err != nil {
			return fmt.Errorf("record job change: %w", err)
		}
		if err := r.AddEmployment(ctx, &domain.PersonEmployment{UserID: userID, PersonID: personID, EmployerName: oldCompany, Title: oldTitle, Confidence: 80}); err != nil {
			return fmt.Errorf("record job change: %w", err)
		}
	}
	if now.Company != "" && now.Title != "" {
		if err := r.AddEmployment(ctx, &domain.PersonEmployment{UserID: userID, PersonID: personID, CompanyID: now.CurrentCompanyID, EmployerName: now.Company, Title: now.Title, IsCurrent: true, Confidence: 80}); err != nil {
			return fmt.Errorf("record job change: %w", err)
		}
	}
	return nil
}

// AddEmployment inserts an employment history record.
func (r *PeopleRepository) AddEmployment(ctx context.Context, emp *domain.PersonEmployment) error {
	if emp == nil || emp.PersonID == uuid.Nil || emp.EmployerName == "" || emp.Title == "" {
		return fmt.Errorf("%w: person_id, employer_name and title are required", domain.ErrValidation)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO person_employments (user_id, person_id, company_id, employer_name, title, start_date, end_date, is_current, location, function, evidence_id, confidence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (person_id, employer_name, title, is_current) DO UPDATE SET
			company_id = COALESCE(EXCLUDED.company_id, person_employments.company_id),
			start_date = COALESCE(EXCLUDED.start_date, person_employments.start_date),
			end_date = COALESCE(EXCLUDED.end_date, person_employments.end_date),
			location = CASE WHEN EXCLUDED.location <> '' THEN EXCLUDED.location ELSE person_employments.location END,
			function = CASE WHEN EXCLUDED.function <> '' THEN EXCLUDED.function ELSE person_employments.function END,
			confidence = EXCLUDED.confidence`,
		emp.UserID, emp.PersonID, emp.CompanyID, emp.EmployerName, emp.Title, emp.StartDate, emp.EndDate, emp.IsCurrent, emp.Location, emp.Function, emp.EvidenceID, emp.Confidence)
	return err
}

// ListEmployments returns employment records for a person.
func (r *PeopleRepository) ListEmployments(ctx context.Context, userID, personID uuid.UUID) ([]domain.PersonEmployment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, person_id, company_id, employer_name, title, start_date, end_date, is_current, location, function, evidence_id, confidence, created_at
		FROM person_employments WHERE user_id = $1 AND person_id = $2 ORDER BY is_current DESC, start_date DESC NULLS LAST, created_at DESC`,
		userID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PersonEmployment
	for rows.Next() {
		var e domain.PersonEmployment
		if err := rows.Scan(&e.ID, &e.UserID, &e.PersonID, &e.CompanyID, &e.EmployerName, &e.Title, &e.StartDate, &e.EndDate, &e.IsCurrent, &e.Location, &e.Function, &e.EvidenceID, &e.Confidence, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
