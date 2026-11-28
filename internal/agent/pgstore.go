package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
)

// PGStore keeps the agent's conversations, turns, memory and gaps in Postgres (migration 000027). Like the
// repositories it takes a Querier, so with the scoped pool each call runs under the owner's row-level security.
type PGStore struct{ pool database.Querier }

// NewPGStore builds a PGStore.
func NewPGStore(pool database.Querier) *PGStore { return &PGStore{pool: pool} }

var _ Store = (*PGStore)(nil)

// GetConversation implements Store.
func (s *PGStore) GetConversation(ctx context.Context, user, id uuid.UUID) (*Conversation, error) {
	var summary string
	var ws []byte
	err := s.pool.QueryRow(ctx, `SELECT summary, working_set FROM agent_conversations WHERE id=$1 AND user_id=$2`, id, user).Scan(&summary, &ws)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load conversation: %w", err)
	}
	c := &Conversation{ID: id, UserID: user, Summary: summary}
	if err := json.Unmarshal(ws, &c.WorkingSet); err != nil {
		return nil, fmt.Errorf("decode working set: %w", err)
	}
	return c, nil
}

// SaveConversation implements Store.
func (s *PGStore) SaveConversation(ctx context.Context, c *Conversation) error {
	ws, _ := json.Marshal(c.WorkingSet)
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO agent_conversations (id, user_id, summary, working_set) VALUES ($1,$2,$3,$4)
		ON CONFLICT (id) DO UPDATE SET summary=EXCLUDED.summary, working_set=EXCLUDED.working_set, updated_at=NOW()
		WHERE agent_conversations.user_id = EXCLUDED.user_id`, c.ID, c.UserID, c.Summary, string(ws))
	if err != nil {
		return fmt.Errorf("save conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("save conversation: the conversation belongs to someone else")
	}
	return nil
}

// AppendTurn implements Store.
func (s *PGStore) AppendTurn(ctx context.Context, t *Turn) error {
	calls := t.ToolCalls
	if len(calls) == 0 {
		calls = json.RawMessage("[]")
	}
	actions, _ := json.Marshal(t.Actions)
	if t.Actions == nil {
		actions = []byte("[]")
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO agent_turns (id, conversation_id, user_id, role, text, tool_calls, actions) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		t.ID, t.ConversationID, t.UserID, t.Role, t.Text, string(calls), string(actions)); err != nil {
		return fmt.Errorf("store turn: %w", err)
	}
	return nil
}

func scanTurns(rows pgx.Rows) ([]Turn, error) {
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		var t Turn
		var calls, actions []byte
		if err := rows.Scan(&t.ID, &t.Seq, &t.ConversationID, &t.UserID, &t.Role, &t.Text, &calls, &actions, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.ToolCalls = calls
		_ = json.Unmarshal(actions, &t.Actions)
		out = append(out, t)
	}
	return out, rows.Err()
}

const turnCols = `id, seq, conversation_id, user_id, role, text, tool_calls, actions, created_at`

// RecentTurns implements Store (oldest first).
func (s *PGStore) RecentTurns(ctx context.Context, user, conv uuid.UUID, n int) ([]Turn, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+turnCols+` FROM (SELECT `+turnCols+` FROM agent_turns WHERE conversation_id=$1 AND user_id=$2 ORDER BY seq DESC LIMIT $3) t ORDER BY seq`, conv, user, n)
	if err != nil {
		return nil, fmt.Errorf("recent turns: %w", err)
	}
	return scanTurns(rows)
}

// TurnsAfter implements Store (oldest first).
func (s *PGStore) TurnsAfter(ctx context.Context, user, conv uuid.UUID, afterSeq int64) ([]Turn, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+turnCols+` FROM agent_turns WHERE conversation_id=$1 AND user_id=$2 AND seq>$3 ORDER BY seq`, conv, user, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("turns after: %w", err)
	}
	return scanTurns(rows)
}

// ListMemory implements Store (newest first).
func (s *PGStore) ListMemory(ctx context.Context, user uuid.UUID, n int) ([]Memory, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, fact, created_at FROM agent_memory WHERE user_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT $2`, user, n)
	if err != nil {
		return nil, fmt.Errorf("list memory: %w", err)
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Fact, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMemory implements Store.
func (s *PGStore) AddMemory(ctx context.Context, user uuid.UUID, fact string, sourceTurn uuid.UUID) (uuid.UUID, error) {
	id := uuid.New()
	// The source turn is stored after the memory in the same request, so it is linked only when it exists.
	if _, err := s.pool.Exec(ctx, `INSERT INTO agent_memory (id, user_id, fact, source_turn_id)
		VALUES ($1,$2,$3,(SELECT id FROM agent_turns WHERE id=$4))`, id, user, fact, sourceTurn); err != nil {
		return uuid.Nil, fmt.Errorf("remember: %w", err)
	}
	return id, nil
}

// SetMemoryDeleted implements Store.
func (s *PGStore) SetMemoryDeleted(ctx context.Context, user, id uuid.UUID, deleted bool) error {
	q := `UPDATE agent_memory SET deleted_at = NULL WHERE id=$1 AND user_id=$2`
	if deleted {
		q = `UPDATE agent_memory SET deleted_at = NOW() WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`
	}
	tag, err := s.pool.Exec(ctx, q, id, user)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("no such memory")
	}
	return nil
}

// AddGap implements Store.
func (s *PGStore) AddGap(ctx context.Context, user uuid.UUID, request string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO agent_gaps (user_id, request) VALUES ($1,$2)`, user, request)
	return err
}

func (s *PGStore) findAction(ctx context.Context, user uuid.UUID, actionID string) (turnID uuid.UUID, raw []byte, actions []Action, idx int, err error) {
	idx = -1
	probe, _ := json.Marshal([]map[string]string{{"id": actionID}})
	err = s.pool.QueryRow(ctx, `SELECT id, actions FROM agent_turns WHERE user_id=$1 AND actions @> $2::jsonb LIMIT 1`, user, string(probe)).Scan(&turnID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, nil, -1, nil
	}
	if err != nil {
		return
	}
	if err = json.Unmarshal(raw, &actions); err != nil {
		return
	}
	for i := range actions {
		if actions[i].ID == actionID {
			idx = i
		}
	}
	return
}

// ClaimUndo implements Store. The update only applies if the turn's actions are unchanged since they were read, so
// two taps at once cannot both claim the undo.
func (s *PGStore) ClaimUndo(ctx context.Context, user uuid.UUID, actionID string) (UndoRecord, bool, error) {
	turnID, raw, actions, idx, err := s.findAction(ctx, user, actionID)
	if err != nil {
		return UndoRecord{}, false, err
	}
	if idx < 0 || actions[idx].Undo == nil || actions[idx].Undo.Used {
		return UndoRecord{}, false, nil
	}
	rec := *actions[idx].Undo
	actions[idx].Undo.Used = true
	next, _ := json.Marshal(actions)
	tag, err := s.pool.Exec(ctx, `UPDATE agent_turns SET actions=$1::jsonb WHERE id=$2 AND actions=$3::jsonb`, string(next), turnID, string(raw))
	if err != nil {
		return UndoRecord{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return UndoRecord{}, false, nil
	}
	return rec, true, nil
}

// ReleaseUndo implements Store.
func (s *PGStore) ReleaseUndo(ctx context.Context, user uuid.UUID, actionID string) error {
	turnID, raw, actions, idx, err := s.findAction(ctx, user, actionID)
	if err != nil || idx < 0 || actions[idx].Undo == nil {
		return err
	}
	actions[idx].Undo.Used = false
	next, _ := json.Marshal(actions)
	_, err = s.pool.Exec(ctx, `UPDATE agent_turns SET actions=$1::jsonb WHERE id=$2 AND actions=$3::jsonb`, string(next), turnID, string(raw))
	return err
}
