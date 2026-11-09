package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// HostTokenPrefix marks a host token so it is recognizable (and greppable in a leak scan).
const HostTokenPrefix = "ghk_"

// HostService manages execution hosts: registration, authentication, presence and claiming.
type HostService struct {
	hosts         repository.HostRepository
	tasks         repository.TaskRepository
	leaseDuration time.Duration
	now           func() time.Time
	logger        *slog.Logger
}

// NewHostService builds a HostService. leaseDuration <= 0 uses 60s.
func NewHostService(hosts repository.HostRepository, tasks repository.TaskRepository, leaseDuration time.Duration, logger *slog.Logger) *HostService {
	if leaseDuration <= 0 {
		leaseDuration = 60 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HostService{hosts: hosts, tasks: tasks, leaseDuration: leaseDuration, now: func() time.Time { return time.Now().UTC() }, logger: logger}
}

// LeaseDuration is the lease a host gets for a claimed task.
func (s *HostService) LeaseDuration() time.Duration { return s.leaseDuration }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate host token: %w", err)
	}
	return HostTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// Register creates a host for the owner and returns its token. The token is shown once; only its hash
// is stored. granted are the capabilities the owner allows this host to ever use.
func (s *HostService) Register(ctx context.Context, userID uuid.UUID, name string, kind domain.HostKind, granted []string) (*domain.Host, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, "", fmt.Errorf("%w: host name must be 1 to 100 characters", domain.ErrValidation)
	}
	if !domain.ValidHostKind(kind) {
		return nil, "", fmt.Errorf("%w: host kind must be laptop or phone", domain.ErrValidation)
	}
	caps, err := domain.NormalizeHostCapabilities(granted)
	if err != nil {
		return nil, "", err
	}
	token, err := newToken()
	if err != nil {
		return nil, "", err
	}
	h, err := s.hosts.CreateHost(ctx, &domain.Host{UserID: userID, Name: name, Kind: kind, Capabilities: caps}, hashToken(token))
	if err != nil {
		return nil, "", err
	}
	return h, token, nil
}

// Authenticate resolves a host token to its host. An unknown or revoked token is ErrUnauthorized-like:
// the caller maps any error to 401 without saying which.
func (s *HostService) Authenticate(ctx context.Context, token string) (*domain.Host, error) {
	if !strings.HasPrefix(token, HostTokenPrefix) || len(token) < len(HostTokenPrefix)+20 {
		return nil, domain.ErrNotFound
	}
	return s.hosts.GetHostByTokenHash(ctx, hashToken(token))
}

// Heartbeat records presence and what the host can currently do.
func (s *HostService) Heartbeat(ctx context.Context, h *domain.Host, advertised []string, version string) error {
	caps, err := domain.NormalizeHostCapabilities(advertised)
	if err != nil {
		return err
	}
	if len(version) > 64 {
		version = version[:64]
	}
	return s.hosts.Touch(ctx, h.ID, caps, version, s.now())
}

// ClaimResult is what a host receives when it claims work.
type ClaimResult struct {
	Task         *domain.Task                  `json:"task"`
	Dependencies []repository.DependencyOutput `json:"dependency_outputs,omitempty"`
	// ApprovedActions lists the approval action types the owner has already granted for this task, so a
	// host that paused to ask can tell, on its next claim, that the exact action it wanted is approved.
	ApprovedActions []string `json:"approved_actions,omitempty"`
}

// MarshalJSON keeps dependency outputs as plain {title, output} objects.
func (c ClaimResult) MarshalJSON() ([]byte, error) {
	type dep struct {
		Title  string          `json:"title"`
		Output json.RawMessage `json:"output"`
	}
	deps := make([]dep, 0, len(c.Dependencies))
	for _, d := range c.Dependencies {
		deps = append(deps, dep{Title: d.Title, Output: d.Output})
	}
	return json.Marshal(struct {
		Task            *domain.Task `json:"task"`
		Dependencies    []dep        `json:"dependency_outputs,omitempty"`
		ApprovedActions []string     `json:"approved_actions,omitempty"`
	}{c.Task, deps, c.ApprovedActions})
}

// Claim leases the next task this host can do, or returns nil when there is none. Claiming counts as
// contact. A paused host is never given work.
func (s *HostService) Claim(ctx context.Context, h *domain.Host) (*ClaimResult, error) {
	if err := s.hosts.Touch(ctx, h.ID, nil, "", s.now()); err != nil {
		return nil, err
	}
	if h.Paused {
		return nil, nil
	}
	task, err := s.tasks.ClaimNextTaskForHost(ctx, h.UserID, h.WorkerID(), h.EffectiveCapabilities(), s.leaseDuration)
	if err != nil {
		return nil, fmt.Errorf("host_service: claim: %w", err)
	}
	if task == nil {
		return nil, nil
	}
	meta, _ := json.Marshal(map[string]any{"worker_id": h.WorkerID(), "host": h.Name, "host_kind": h.Kind, "lease_expires_at": task.LeaseExpiresAt})
	_ = s.tasks.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    task.ID,
		EventType: "STATUS_CHANGE",
		Message:   fmt.Sprintf("Task claimed by %s host %q", h.Kind, h.Name),
		Metadata:  meta,
	})
	// The Attempts tab shows host runs like any other run: one attempt per try, upserted by a stable id.
	_ = s.tasks.RecordAttempt(ctx, &domain.TaskAttempt{
		ID:            hostAttemptID(task.ID, task.RetryCount+1),
		TaskID:        task.ID,
		WorkerID:      h.WorkerID(),
		AttemptNumber: task.RetryCount + 1,
		Status:        "RUNNING",
		StartedAt:     s.now(),
	})
	res := &ClaimResult{Task: task}
	if deps, derr := s.tasks.GetDependencyOutputs(ctx, task.ID); derr != nil {
		s.logger.Warn("host claim: could not load dependency outputs", "task_id", task.ID, "error", derr)
	} else {
		res.Dependencies = deps
	}
	return res, nil
}

// WaitingTask is a queued task that needs a host and the reason it is not running.
type WaitingTask struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Requires []string  `json:"requires"`
	// Covered is true when a live host can take it now (it is only waiting for that host's next claim).
	Covered bool `json:"covered"`
}

// HostView is a host as shown to the owner.
type HostView struct {
	*domain.Host
	Live      bool     `json:"live"`
	Effective []string `json:"effective_capabilities"`
}

// HostOverview is the owner's picture of which device is primary and what is waiting for one.
type HostOverview struct {
	Mode    domain.HostMode `json:"mode"`
	Hosts   []HostView      `json:"hosts"`
	Waiting []WaitingTask   `json:"waiting"`
}

// Overview returns the owner's hosts, the elected mode, and tasks waiting for a host.
func (s *HostService) Overview(ctx context.Context, userID uuid.UUID) (*HostOverview, error) {
	hosts, err := s.hosts.ListHosts(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := &HostOverview{Mode: domain.ElectMode(hosts, now), Hosts: make([]HostView, 0, len(hosts)), Waiting: []WaitingTask{}}
	for _, h := range hosts {
		eff := h.EffectiveCapabilities()
		if eff == nil {
			eff = []string{}
		}
		out.Hosts = append(out.Hosts, HostView{Host: h, Live: h.IsLive(now), Effective: eff})
	}
	tasks, err := s.tasks.ListTasksNeedingHosts(ctx, userID, 50)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		req := domain.RequiredCapabilities(t.InputPayload)
		out.Waiting = append(out.Waiting, WaitingTask{ID: t.ID, Title: t.Title, Requires: req, Covered: domain.AnyHostCovers(hosts, req, now)})
	}
	return out, nil
}

// CanCover reports whether one of the owner's live devices can do work that needs these capabilities.
func (s *HostService) CanCover(ctx context.Context, userID uuid.UUID, required []string) bool {
	hosts, err := s.hosts.ListHosts(ctx, userID)
	if err != nil {
		return true // if we cannot tell, do not claim a device is missing
	}
	return domain.AnyHostCovers(hosts, required, s.now())
}

// SetPaused pauses or resumes a host.
func (s *HostService) SetPaused(ctx context.Context, userID, id uuid.UUID, paused bool) error {
	return s.hosts.SetPaused(ctx, userID, id, paused)
}

// SetGranted replaces the capabilities the owner grants a host.
func (s *HostService) SetGranted(ctx context.Context, userID, id uuid.UUID, caps []string) error {
	norm, err := domain.NormalizeHostCapabilities(caps)
	if err != nil {
		return err
	}
	return s.hosts.SetGrantedCapabilities(ctx, userID, id, norm)
}

// Revoke permanently disables a host's token.
func (s *HostService) Revoke(ctx context.Context, userID, id uuid.UUID) error {
	return s.hosts.Revoke(ctx, userID, id, s.now())
}

// AnyLiveAgentHost reports whether some live host in the current scope can run an agent. The cloud runner
// is not woken for plain work while one is.
func (s *HostService) AnyLiveAgentHost(ctx context.Context) (bool, error) {
	now := s.now()
	hosts, err := s.hosts.ListLiveCandidates(ctx, now.Add(-domain.HostLiveWindow))
	if err != nil {
		return false, err
	}
	return domain.AnyHostCovers(hosts, nil, now), nil
}

// RunnerWakeupFunc is the wake contract the API uses; *runner.WakeupController satisfies it.
type RunnerWakeupFunc interface {
	TriggerWakeup(ctx context.Context) error
}

// HostPresence reports whether a live host can take plain work.
type HostPresence interface {
	AnyLiveAgentHost(ctx context.Context) (bool, error)
}

// HostAwareWakeup does not wake the cloud runner while a live host can do the work. If presence cannot
// be determined it wakes the runner: a needless wake costs cents, a task stuck behind an unknown does not.
type HostAwareWakeup struct {
	inner    RunnerWakeupFunc
	presence HostPresence
	logger   *slog.Logger
}

// NewHostAwareWakeup wraps inner. A nil inner or presence returns the other unchanged behavior.
func NewHostAwareWakeup(inner RunnerWakeupFunc, presence HostPresence, logger *slog.Logger) *HostAwareWakeup {
	if logger == nil {
		logger = slog.Default()
	}
	return &HostAwareWakeup{inner: inner, presence: presence, logger: logger}
}

// TriggerWakeup implements the wake contract.
func (w *HostAwareWakeup) TriggerWakeup(ctx context.Context) error {
	if w.inner == nil {
		return nil
	}
	if w.presence != nil {
		live, err := w.presence.AnyLiveAgentHost(ctx)
		switch {
		case err != nil:
			w.logger.Warn("host presence unavailable; waking the runner", "error", err)
		case live:
			w.logger.Info("a live host can take the work; not waking the runner")
			return nil
		}
	}
	return w.inner.TriggerWakeup(ctx)
}

// hostAttemptID is the stable id of a host's attempt at a task, so claim and finish update one record.
func hostAttemptID(taskID uuid.UUID, attemptNumber int) uuid.UUID {
	return uuid.NewSHA1(taskID, []byte(fmt.Sprintf("host-attempt:%d", attemptNumber)))
}

// AttemptHandle identifies the attempt a host is finishing. Take it BEFORE completing or failing the
// task: a retryable failure increments the retry count, which would change the attempt number.
type AttemptHandle struct {
	id      uuid.UUID
	taskID  uuid.UUID
	number  int
	started time.Time
}

// BeginFinish captures the attempt for a task the host is about to finish.
func (s *HostService) BeginFinish(ctx context.Context, h *domain.Host, taskID uuid.UUID) *AttemptHandle {
	task, err := s.tasks.GetTaskByID(ctx, taskID)
	if err != nil || task == nil {
		return nil
	}
	ah := &AttemptHandle{id: hostAttemptID(taskID, task.RetryCount+1), taskID: taskID, number: task.RetryCount + 1, started: s.now()}
	if attempts, err := s.tasks.GetAttempts(ctx, taskID); err == nil {
		for _, a := range attempts {
			if a.ID == ah.id {
				ah.started = a.StartedAt
			}
		}
	}
	return ah
}

// EndFinish records the outcome. Call it only after the task transition succeeded, so a refused
// completion (not the lease holder) leaves no attempt behind. A recording failure is logged, never fatal.
func (s *HostService) EndFinish(ctx context.Context, h *domain.Host, ah *AttemptHandle, status string, errMsg string) {
	if ah == nil {
		return
	}
	now := s.now()
	a := &domain.TaskAttempt{
		ID: ah.id, TaskID: ah.taskID, WorkerID: h.WorkerID(), AttemptNumber: ah.number,
		Status: status, StartedAt: ah.started, CompletedAt: &now, DurationMs: int(now.Sub(ah.started).Milliseconds()),
	}
	if errMsg != "" {
		a.ErrorMessage = &errMsg
	}
	if err := s.tasks.RecordAttempt(ctx, a); err != nil {
		s.logger.Error("host attempt not recorded", "task_id", ah.taskID, "error", err)
	}
}

// LeasedTask returns the task only if this host holds its lease and it is running. A host may ask for an
// approval only on work it is actually doing; anything else is refused as not found so that one host
// cannot learn about another's tasks.
func (s *HostService) LeasedTask(ctx context.Context, h *domain.Host, taskID uuid.UUID) (*domain.Task, error) {
	task, err := s.tasks.GetTaskByID(ctx, taskID)
	if err != nil || task == nil {
		return nil, domain.ErrNotFound
	}
	if task.UserID != h.UserID || task.LeasedBy == nil || *task.LeasedBy != h.WorkerID() || task.Status != domain.TaskStatusRunning {
		return nil, domain.ErrNotFound
	}
	return task, nil
}
