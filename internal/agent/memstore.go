package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemStore is an in-memory Store for tests and the dry-run eval.
type MemStore struct {
	mu    sync.Mutex
	convs map[uuid.UUID]*Conversation
	turns []Turn
	mem   map[uuid.UUID]*Memory
	del   map[uuid.UUID]bool
	gaps  []string
	seq   int64
}

func NewMemStore() *MemStore {
	return &MemStore{convs: map[uuid.UUID]*Conversation{}, mem: map[uuid.UUID]*Memory{}, del: map[uuid.UUID]bool{}}
}

func (m *MemStore) GetConversation(_ context.Context, user, id uuid.UUID) (*Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.convs[id]
	if c == nil || c.UserID != user {
		return nil, nil
	}
	cp := *c
	b, _ := json.Marshal(c.WorkingSet)
	_ = json.Unmarshal(b, &cp.WorkingSet)
	return &cp, nil
}
func (m *MemStore) SaveConversation(_ context.Context, c *Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	b, _ := json.Marshal(c.WorkingSet)
	cp.WorkingSet = WorkingSet{}
	_ = json.Unmarshal(b, &cp.WorkingSet)
	m.convs[c.ID] = &cp
	return nil
}
func (m *MemStore) AppendTurn(_ context.Context, t *Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	cp := *t
	cp.Seq = m.seq
	m.turns = append(m.turns, cp)
	return nil
}
func (m *MemStore) forConv(conv uuid.UUID, after int64) []Turn {
	var out []Turn
	for _, t := range m.turns {
		if t.ConversationID == conv && t.Seq > after {
			out = append(out, t)
		}
	}
	return out
}
func (m *MemStore) RecentTurns(_ context.Context, _, conv uuid.UUID, n int) ([]Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.forConv(conv, 0)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}
func (m *MemStore) TurnsAfter(_ context.Context, _, conv uuid.UUID, after int64) ([]Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.forConv(conv, after), nil
}
func (m *MemStore) ListMemory(_ context.Context, _ uuid.UUID, n int) ([]Memory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Memory
	for id, x := range m.mem {
		if !m.del[id] {
			out = append(out, *x)
		}
	}
	return out, nil
}
func (m *MemStore) AddMemory(_ context.Context, _ uuid.UUID, fact string, _ uuid.UUID) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New()
	m.mem[id] = &Memory{ID: id, Fact: fact, CreatedAt: time.Now()}
	return id, nil
}
func (m *MemStore) SetMemoryDeleted(_ context.Context, _, id uuid.UUID, deleted bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mem[id] == nil {
		return errors.New("no such memory")
	}
	m.del[id] = deleted
	return nil
}
func (m *MemStore) AddGap(_ context.Context, _ uuid.UUID, r string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gaps = append(m.gaps, r)
	return nil
}
func (m *MemStore) ClaimUndo(_ context.Context, _ uuid.UUID, id string) (UndoRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.turns {
		for j := range m.turns[i].Actions {
			a := &m.turns[i].Actions[j]
			if a.ID == id && a.Undo != nil && !a.Undo.Used {
				a.Undo.Used = true
				return *a.Undo, true, nil
			}
		}
	}
	return UndoRecord{}, false, nil
}
func (m *MemStore) ReleaseUndo(_ context.Context, _ uuid.UUID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.turns {
		for j := range m.turns[i].Actions {
			if m.turns[i].Actions[j].ID == id && m.turns[i].Actions[j].Undo != nil {
				m.turns[i].Actions[j].Undo.Used = false
			}
		}
	}
	return nil
}
