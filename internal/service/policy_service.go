package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/repository"
)

// PolicyService reads and writes the owner's permission policy.
type PolicyService struct {
	repo repository.PolicyRepository
	now  func() time.Time
}

// NewPolicyService builds a PolicyService.
func NewPolicyService(repo repository.PolicyRepository) *PolicyService {
	return &PolicyService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// PolicyView is a policy with its version. Version 0 means the owner has not saved one: the default applies.
type PolicyView struct {
	Policy  policy.Policy `json:"policy"`
	Version int           `json:"version"`
}

// Get returns the owner's policy, or the default when none is saved. A stored policy that no longer
// validates is not trusted: the default (ask) is returned instead of anything looser.
func (s *PolicyService) Get(ctx context.Context, userID uuid.UUID) (*PolicyView, error) {
	sp, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if sp.Version == 0 || len(sp.Policy) == 0 {
		return &PolicyView{Policy: policy.DefaultPolicy(), Version: 0}, nil
	}
	var p policy.Policy
	if err := json.Unmarshal(sp.Policy, &p); err != nil {
		return &PolicyView{Policy: policy.DefaultPolicy(), Version: sp.Version}, nil
	}
	// Stored Full windows are validated against their own creation time, so only check shape here.
	probe := p
	if probe.FullUntil != nil {
		probe.FullUntil = nil
	}
	if err := probe.Validate(s.now()); err != nil {
		return &PolicyView{Policy: policy.DefaultPolicy(), Version: sp.Version}, nil
	}
	return &PolicyView{Policy: p, Version: sp.Version}, nil
}

// Version returns just the stored version (0 when none), for the host heartbeat.
func (s *PolicyService) Version(ctx context.Context, userID uuid.UUID) (int, error) {
	sp, err := s.repo.Get(ctx, userID)
	return sp.Version, err
}

// Put validates and stores a policy. expected is the version the caller last read.
func (s *PolicyService) Put(ctx context.Context, userID uuid.UUID, p policy.Policy, expected int) (*PolicyView, error) {
	if expected < 0 {
		return nil, fmt.Errorf("%w: version cannot be negative", domain.ErrValidation)
	}
	if err := p.Validate(s.now()); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	v, err := s.repo.Put(ctx, userID, raw, expected)
	if err != nil {
		return nil, err
	}
	return &PolicyView{Policy: p, Version: v}, nil
}
