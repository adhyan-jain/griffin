package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/domain"
)

var domainLike = regexp.MustCompile(`^(https?://)?([a-z0-9-]+\.)+[a-z]{2,}(/\S*)?$`)

// hostOf returns the bare host of a domain or URL ("https://www.Acme.io/x" is "acme.io"), or "" when the text is
// not one.
func hostOf(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || !domainLike.MatchString(s) {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// ResolveCompany finds the owner's company for a name, an alias or a domain, with the rule the opportunity
// ingestion uses: domain.NormalizeEmployer ("Acme Inc", "ACME" and "Acme Technologies Pvt. Ltd." are one company),
// and a known domain matches its company. A domain such as "acme.io" matches the company with that domain, else
// the company whose normalized name is its first label. A company no row matches is added as a candidate, exactly
// as the scan adds one. It never creates a second row for a company that exists.
func (r *OpportunityRepository) ResolveCompany(ctx context.Context, userID uuid.UUID, nameOrDomain string) (*domain.Company, error) {
	text := strings.TrimSpace(nameOrDomain)
	if text == "" {
		return nil, fmt.Errorf("%w: a company name is required", domain.ErrValidation)
	}
	name, host := text, hostOf(text)
	if host != "" {
		var id uuid.UUID
		err := r.pool.QueryRow(ctx, `SELECT id FROM companies WHERE user_id = $1 AND lower(domain) = $2 ORDER BY created_at LIMIT 1`, userID, host).Scan(&id)
		if err == nil {
			return r.getCompany(ctx, userID, id)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("resolve company: %w", err)
		}
		name = strings.Split(host, ".")[0]
	}
	c := &domain.Company{UserID: userID, Name: name, NormalizedName: domain.NormalizeEmployer(name), Kind: "unknown", DiscoveredVia: "research", Status: domain.CompanyCandidate}
	if host != "" {
		c.Domain = &host
	}
	if c.NormalizedName == "" {
		return nil, fmt.Errorf("%w: %q is not a company name", domain.ErrValidation, text)
	}
	if err := r.UpsertCompany(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (r *OpportunityRepository) getCompany(ctx context.Context, userID, id uuid.UUID) (*domain.Company, error) {
	c := &domain.Company{UserID: userID}
	err := r.pool.QueryRow(ctx, `SELECT id, name, normalized_name, domain, kind, discovered_via, evidence_url, status, created_at, updated_at FROM companies WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&c.ID, &c.Name, &c.NormalizedName, &c.Domain, &c.Kind, &c.DiscoveredVia, &c.EvidenceURL, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get company: %w", err)
	}
	return c, nil
}

// AddCompanyObservation stores one observation on a company the owner has. A new value adds a row and never
// overwrites an older one; the same value from the same source is stored once (it reports whether it was new).
func (r *OpportunityRepository) AddCompanyObservation(ctx context.Context, o *domain.CompanyObservation) (bool, error) {
	if !domain.IsCompanyProperty(o.Property) {
		return false, fmt.Errorf("%w: %q is not a company property", domain.ErrValidation, o.Property)
	}
	if strings.TrimSpace(o.Value) == "" {
		return false, fmt.Errorf("%w: an observation needs a value", domain.ErrValidation)
	}
	if o.ClaimOrigin == "" {
		o.ClaimOrigin = domain.OriginUnknown
	}
	if o.ObservedAt.IsZero() {
		o.ObservedAt = time.Now().UTC()
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO company_observations (user_id, company_id, property, value, evidence_id, source_url, claim_origin, observed_at)
		SELECT $1::uuid, c.id, $3, $4, $5, $6, $7, $8 FROM companies c WHERE c.id = $2::uuid AND c.user_id = $1::uuid
		ON CONFLICT (company_id, property, value, source_url) DO NOTHING
		RETURNING id`,
		o.UserID, o.CompanyID, o.Property, o.Value, o.EvidenceID, o.SourceURL, o.ClaimOrigin, o.ObservedAt).Scan(&o.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		var owned bool
		if qerr := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM companies WHERE id = $1 AND user_id = $2)`, o.CompanyID, o.UserID).Scan(&owned); qerr != nil {
			return false, fmt.Errorf("add company observation: %w", qerr)
		}
		if !owned {
			return false, fmt.Errorf("%w: the company does not belong to the user", domain.ErrNotFound)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("add company observation: %w", err)
	}
	return true, nil
}

// CompanySummary is a company row for the list: the company, its open listings, and the latest observation of
// each property.
type CompanySummary struct {
	domain.Company
	OpenListings int                          `json:"open_listings"`
	Latest       []*domain.CompanyObservation `json:"latest_observations"`
}

// ListCompanySummaries lists the owner's companies (newest first, at most limit), each with its open-listing count
// and the latest observation per property.
func (r *OpportunityRepository) ListCompanySummaries(ctx context.Context, userID uuid.UUID, limit int) ([]*CompanySummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.name, c.normalized_name, c.domain, c.kind, c.discovered_via, c.evidence_url, c.status, c.created_at, c.updated_at,
		       (SELECT count(*) FROM job_discoveries j WHERE j.user_id = c.user_id AND j.company_id = c.id AND j.stage <> 'closed' AND j.closed_at IS NULL)
		  FROM companies c WHERE c.user_id = $1 ORDER BY c.created_at DESC, c.name LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list company summaries: %w", err)
	}
	defer rows.Close()
	out := []*CompanySummary{}
	byID := map[uuid.UUID]*CompanySummary{}
	for rows.Next() {
		s := &CompanySummary{Latest: []*domain.CompanyObservation{}}
		s.UserID = userID
		if err := rows.Scan(&s.ID, &s.Name, &s.NormalizedName, &s.Domain, &s.Kind, &s.DiscoveredVia, &s.EvidenceURL, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.OpenListings); err != nil {
			return nil, fmt.Errorf("scan company summary: %w", err)
		}
		out = append(out, s)
		byID[s.ID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	obs, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (o.company_id, o.property) o.id, o.company_id, o.property, o.value, o.evidence_id, o.source_url, o.claim_origin, o.observed_at
		  FROM company_observations o
		 WHERE o.user_id = $1 AND o.company_id IN (SELECT id FROM companies WHERE user_id = $1 ORDER BY created_at DESC, name LIMIT $2)
		 ORDER BY o.company_id, o.property, o.observed_at DESC, o.created_at DESC`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list latest observations: %w", err)
	}
	defer obs.Close()
	now := time.Now()
	for obs.Next() {
		o, err := scanObservation(obs, now)
		if err != nil {
			return nil, err
		}
		if s := byID[o.CompanyID]; s != nil {
			s.Latest = append(s.Latest, o)
		}
	}
	return out, obs.Err()
}

func scanObservation(row pgx.Row, now time.Time) (*domain.CompanyObservation, error) {
	o := &domain.CompanyObservation{}
	if err := row.Scan(&o.ID, &o.CompanyID, &o.Property, &o.Value, &o.EvidenceID, &o.SourceURL, &o.ClaimOrigin, &o.ObservedAt); err != nil {
		return nil, fmt.Errorf("scan observation: %w", err)
	}
	o.AgeDays = int(now.Sub(o.ObservedAt).Hours() / 24)
	return o, nil
}

// CompanyListing is one of a company's listings, with the link to the employer's own posting when one is known.
type CompanyListing struct {
	ID           uuid.UUID  `json:"id"`
	Role         string     `json:"role"`
	Stage        string     `json:"stage"`
	Location     string     `json:"location"`
	Pay          string     `json:"pay"`
	URL          string     `json:"url"`
	OfficialURL  *string    `json:"official_url"`
	SourceKind   string     `json:"source_kind"`
	Verification string     `json:"verification"`
	Open         bool       `json:"open"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
}

// CompanyEvidence is a research finding that backs an observation.
type CompanyEvidence struct {
	ID          uuid.UUID `json:"id"`
	Claim       string    `json:"claim"`
	Value       string    `json:"value"`
	Quote       string    `json:"quote"`
	SourceURL   string    `json:"source_url"`
	Status      string    `json:"status"`
	Confidence  int       `json:"confidence"`
	RetrievedAt time.Time `json:"retrieved_at"`
}

// CompanyDetail is one company with its listings, its whole observation history and the evidence behind it.
type CompanyDetail struct {
	Company      *domain.Company              `json:"company"`
	OpenListings int                          `json:"open_listings"`
	Listings     []*CompanyListing            `json:"listings"`
	Observations []*domain.CompanyObservation `json:"observations"`
	Evidence     []*CompanyEvidence           `json:"evidence"`
}

// GetCompanyDetail returns one company of the owner's; domain.ErrNotFound for any other id.
func (r *OpportunityRepository) GetCompanyDetail(ctx context.Context, userID, id uuid.UUID) (*CompanyDetail, error) {
	c, err := r.getCompany(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	d := &CompanyDetail{Company: c, Listings: []*CompanyListing{}, Observations: []*domain.CompanyObservation{}, Evidence: []*CompanyEvidence{}}

	lrows, err := r.pool.Query(ctx, `
		SELECT id, role, stage, location, pay, url, source_kind, verification, (stage <> 'closed' AND closed_at IS NULL), last_seen_at,
		       CASE WHEN employer_url IS NOT NULL AND employer_url <> '' THEN employer_url
		            WHEN source_kind IN ('employer_ats', 'employer_site') THEN url END
		  FROM job_discoveries WHERE user_id = $1 AND company_id = $2
		 ORDER BY (stage <> 'closed' AND closed_at IS NULL) DESC, first_seen_at DESC LIMIT 200`, userID, id)
	if err != nil {
		return nil, fmt.Errorf("company listings: %w", err)
	}
	defer lrows.Close()
	for lrows.Next() {
		l := &CompanyListing{}
		if err := lrows.Scan(&l.ID, &l.Role, &l.Stage, &l.Location, &l.Pay, &l.URL, &l.SourceKind, &l.Verification, &l.Open, &l.LastSeenAt, &l.OfficialURL); err != nil {
			return nil, fmt.Errorf("scan company listing: %w", err)
		}
		if l.Open {
			d.OpenListings++
		}
		d.Listings = append(d.Listings, l)
	}
	if err := lrows.Err(); err != nil {
		return nil, err
	}
	lrows.Close()

	orows, err := r.pool.Query(ctx, `
		SELECT id, company_id, property, value, evidence_id, source_url, claim_origin, observed_at
		  FROM company_observations WHERE user_id = $1 AND company_id = $2 ORDER BY property, observed_at DESC, created_at DESC`, userID, id)
	if err != nil {
		return nil, fmt.Errorf("company observations: %w", err)
	}
	defer orows.Close()
	now := time.Now()
	for orows.Next() {
		o, err := scanObservation(orows, now)
		if err != nil {
			return nil, err
		}
		d.Observations = append(d.Observations, o)
	}
	if err := orows.Err(); err != nil {
		return nil, err
	}
	orows.Close()

	erows, err := r.pool.Query(ctx, `
		SELECT e.id, e.claim, e.extracted_value, e.excerpt_quote, e.source_url, e.status, e.confidence, e.retrieved_at
		  FROM evidence_items e
		 WHERE e.user_id = $1 AND e.id IN (SELECT evidence_id FROM company_observations WHERE user_id = $1 AND company_id = $2 AND evidence_id IS NOT NULL)
		 ORDER BY e.retrieved_at DESC`, userID, id)
	if err != nil {
		return nil, fmt.Errorf("company evidence: %w", err)
	}
	defer erows.Close()
	for erows.Next() {
		e := &CompanyEvidence{}
		if err := erows.Scan(&e.ID, &e.Claim, &e.Value, &e.Quote, &e.SourceURL, &e.Status, &e.Confidence, &e.RetrievedAt); err != nil {
			return nil, fmt.Errorf("scan company evidence: %w", err)
		}
		d.Evidence = append(d.Evidence, e)
	}
	return d, erows.Err()
}
