-- 000027 down: remove the JJ agent's conversation store.
DROP TABLE IF EXISTS agent_gaps;
DROP TABLE IF EXISTS agent_memory;
DROP TABLE IF EXISTS agent_turns;
DROP TABLE IF EXISTS agent_conversations;
