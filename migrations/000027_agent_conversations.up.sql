-- -----------------------------------------------------------------------------
-- 000027: JJ agent: server-side conversations, turns, memory facts and capability gaps.
-- The server owns chat history so phone, web and voice share one memory. Every table is per user (RLS).
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS agent_conversations (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    summary     TEXT NOT NULL DEFAULT '',
    working_set JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_conversations_user ON agent_conversations (user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS agent_turns (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seq             BIGSERIAL,
    conversation_id UUID NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    text            TEXT NOT NULL DEFAULT '',
    tool_calls      JSONB NOT NULL DEFAULT '[]'::jsonb,
    actions         JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_turns_conversation ON agent_turns (conversation_id, seq);
CREATE INDEX IF NOT EXISTS idx_agent_turns_user ON agent_turns (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_memory (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    fact           TEXT NOT NULL,
    source_turn_id UUID NULL REFERENCES agent_turns(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_memory_user ON agent_memory (user_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS agent_gaps (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    request    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_gaps_user ON agent_gaps (user_id, created_at DESC);

ALTER TABLE agent_conversations ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_conversations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS agent_conversations_user_policy ON agent_conversations;
CREATE POLICY agent_conversations_user_policy ON agent_conversations FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

ALTER TABLE agent_turns ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_turns FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS agent_turns_user_policy ON agent_turns;
CREATE POLICY agent_turns_user_policy ON agent_turns FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

ALTER TABLE agent_memory ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_memory FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS agent_memory_user_policy ON agent_memory;
CREATE POLICY agent_memory_user_policy ON agent_memory FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

ALTER TABLE agent_gaps ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_gaps FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS agent_gaps_user_policy ON agent_gaps;
CREATE POLICY agent_gaps_user_policy ON agent_gaps FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);
