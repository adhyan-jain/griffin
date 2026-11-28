package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ConversationSummary is one row of the owner's recent-chats list.
type ConversationSummary struct {
	ID        uuid.UUID
	UpdatedAt time.Time
	Preview   string // the first user message
}

// ListConversations returns the owner's most recently active conversations that have a user message, newest first.
func (s *PGStore) ListConversations(ctx context.Context, user uuid.UUID, limit int) ([]ConversationSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.updated_at,
		       COALESCE((SELECT t.text FROM agent_turns t WHERE t.conversation_id = c.id AND t.role = 'user' ORDER BY t.seq LIMIT 1), '')
		FROM agent_conversations c
		WHERE c.user_id = $1
		  AND EXISTS (SELECT 1 FROM agent_turns t WHERE t.conversation_id = c.id AND t.role = 'user')
		ORDER BY c.updated_at DESC LIMIT $2`, user, limit)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	var out []ConversationSummary
	for rows.Next() {
		var c ConversationSummary
		if err := rows.Scan(&c.ID, &c.UpdatedAt, &c.Preview); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConversationTurns returns every turn of one of the owner's conversations, oldest first. found is false when the
// conversation does not exist or belongs to someone else.
func (s *PGStore) ConversationTurns(ctx context.Context, user, conv uuid.UUID) (turns []Turn, found bool, err error) {
	c, err := s.GetConversation(ctx, user, conv)
	if err != nil || c == nil {
		return nil, false, err
	}
	turns, err = s.TurnsAfter(ctx, user, conv, 0)
	return turns, true, err
}
