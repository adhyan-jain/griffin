package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// memListings is an in-memory job_discoveries table with the companies, boards, scan history, snapshots and
// evidence around it. It follows the repository's rules (one match order, never a downgrade) so the service's rules
// can be tested without a database; the repository is tested against Postgres in tests/.
type memListings struct {
	rows      []*domain.Listing
	companies []*domain.Company
	boards    []*domain.ATSBoard
	scans     []*domain.SourceScan
	snapshots []*domain.JobSnapshot
	evidence  []*domain.ListingEvidence
	failOn    string // a company name whose listing cannot be saved
}

// legacy Upsert: one per company and role.
func (m *memListings) Upsert(_ context.Context, l *domain.Listing) (bool, error) {
	for _, r := range m.rows {
		if strings.EqualFold(r.Company, l.Company) && strings.EqualFold(r.Role, l.Role) {
			*l = *r
			return false, nil
		}
	}
	l.ID = uuid.New()
	m.rows = append(m.rows, l)
	return true, nil
}

func (m *memListings) List(_ context.Context, _ uuid.UUID, stages []string, _ int) ([]*domain.Listing, error) {
	var out []*domain.Listing
	for _, r := range m.rows {
		if len(stages) == 0 { // like the table: no stages means every listing
			out = append(out, r)
			continue
		}
		for _, s := range stages {
			if r.Stage == s {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (m *memListings) Get(_ context.Context, _ uuid.UUID, id uuid.UUID) (*domain.Listing, error) {
	for _, r := range m.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *memListings) Move(_ context.Context, _ uuid.UUID, id uuid.UUID, from, to, note string, _ *uuid.UUID) error {
	for _, r := range m.rows {
		if r.ID == id && r.Stage == from && domain.CanMove(from, to) {
			r.Stage, r.StageNote = to, note
			if to == domain.StageClosed && r.ClosedAt == nil {
				now := time.Now()
				r.ClosedAt = &now
			} else if from == domain.StageClosed {
				r.ClosedAt = nil
			}
			return nil
		}
	}
	return errors.New("illegal move")
}

func (m *memListings) Checked(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (m *memListings) BackfillIdentity(_ context.Context, _ uuid.UUID) error {
	for _, r := range m.rows {
		if r.EmployerKey == "" {
			id := domain.NewIdentity(r.Source, r.ExternalID, r.Company, r.Role, r.URL)
			r.EmployerKey, r.RoleKey = id.EmployerKey, id.RoleKey
			if id.CanonicalURL != "" && r.CanonicalURL == nil {
				c := id.CanonicalURL
				r.CanonicalURL = &c
			}
		}
	}
	return nil
}

func (m *memListings) UpsertCompany(_ context.Context, c *domain.Company) error {
	for _, x := range m.companies {
		if x.NormalizedName == c.NormalizedName {
			if x.Domain == nil {
				x.Domain = c.Domain
			}
			if x.Status == domain.CompanyCandidate && c.Status == domain.CompanyActive {
				x.Status, x.EvidenceURL = domain.CompanyActive, c.EvidenceURL
			}
			*c = *x
			return nil
		}
	}
	c.ID = uuid.New()
	m.companies = append(m.companies, c)
	return nil
}

func (m *memListings) UpsertBoard(_ context.Context, b *domain.ATSBoard) error {
	for _, x := range m.boards {
		if x.Provider == b.Provider && x.TokenOrURL == b.TokenOrURL {
			*b = *x
			return nil
		}
	}
	b.ID, b.Status = uuid.New(), domain.BoardActive
	m.boards = append(m.boards, b)
	return nil
}

func (m *memListings) RecordBoardScan(_ context.Context, _ uuid.UUID, provider, token, status string) error {
	for _, b := range m.boards {
		if b.Provider != provider || b.TokenOrURL != token {
			continue
		}
		now := time.Now()
		b.LastScanAt = &now
		switch status {
		case "ok":
			b.LastOKAt, b.ConsecutiveFailures, b.Status = &now, 0, domain.BoardActive
		case "failed":
			b.ConsecutiveFailures++
			if b.ConsecutiveFailures >= domain.BoardBrokenAfter {
				b.Status = domain.BoardBroken
			}
		}
	}
	return nil
}

func (m *memListings) RecordScan(_ context.Context, s *domain.SourceScan) error {
	s.ID = uuid.New()
	cp := *s
	m.scans = append(m.scans, &cp)
	return nil
}

func (m *memListings) FindListing(_ context.Context, _ uuid.UUID, id domain.Identity) (*domain.Listing, error) {
	if m.failOn != "" && strings.EqualFold(id.EmployerKey, domain.NormalizeEmployer(m.failOn)) {
		return nil, errors.New("database unavailable")
	}
	for rank := 0; rank < 3; rank++ {
		for _, r := range m.rows {
			switch {
			case rank == 0 && id.ExternalID != "" && r.Source == id.Source && r.ExternalID == id.ExternalID:
				return r, nil
			case rank == 1 && id.CanonicalURL != "" && r.CanonicalURL != nil && *r.CanonicalURL == id.CanonicalURL:
				return r, nil
			case rank == 2 && id.EmployerKey != "" && id.RoleKey != "" && r.EmployerKey == id.EmployerKey && r.RoleKey == id.RoleKey:
				return r, nil
			}
		}
	}
	return nil, nil
}

func (m *memListings) InsertListing(_ context.Context, l *domain.Listing) error {
	l.ID = uuid.New()
	l.Stage = domain.StageVerified
	now := time.Now()
	l.CreatedAt, l.UpdatedAt = now, now
	cp := *l
	m.rows = append(m.rows, &cp)
	return nil
}

func (m *memListings) UpdateListing(_ context.Context, l *domain.Listing) error {
	for i, r := range m.rows {
		if r.ID == l.ID {
			cp := *l
			// stage, stage note, task and close state are the pipeline's, not the scan's
			cp.Stage, cp.StageNote, cp.TaskID, cp.ClosedAt, cp.ReopenedCount = r.Stage, r.StageNote, r.TaskID, r.ClosedAt, r.ReopenedCount
			if l.Description == "" {
				cp.Description = r.Description
			}
			m.rows[i] = &cp
			return nil
		}
	}
	return domain.ErrNotFound
}

func (m *memListings) Reopen(_ context.Context, _ uuid.UUID, id uuid.UUID, note string) error {
	for _, r := range m.rows {
		if r.ID == id && r.ClosedAt != nil {
			if r.Stage == domain.StageClosed {
				r.Stage, r.StageNote = domain.StageVerified, note
			}
			r.ClosedAt = nil
			r.ReopenedCount++
		}
	}
	return nil
}

func (m *memListings) SetClosedAt(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	for _, r := range m.rows {
		if r.ID == id && r.ClosedAt == nil {
			now := time.Now()
			r.ClosedAt = &now
		}
	}
	return nil
}

func (m *memListings) OpenFromTarget(_ context.Context, _ uuid.UUID, source, target string) ([]*domain.Listing, error) {
	var out []*domain.Listing
	for _, r := range m.rows {
		if r.Source == source && r.SourceTarget == target && r.ClosedAt == nil && r.Stage != domain.StageClosed {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memListings) AddSnapshot(_ context.Context, s *domain.JobSnapshot) error {
	s.ID = uuid.New()
	m.snapshots = append(m.snapshots, s)
	return nil
}

func (m *memListings) AddEvidence(_ context.Context, e *domain.ListingEvidence) error {
	e.ID = uuid.New()
	m.evidence = append(m.evidence, e)
	return nil
}

func (m *memListings) snapshotsOf(id uuid.UUID) []*domain.JobSnapshot {
	var out []*domain.JobSnapshot
	for _, s := range m.snapshots {
		if s.ListingID == id {
			out = append(out, s)
		}
	}
	return out
}
