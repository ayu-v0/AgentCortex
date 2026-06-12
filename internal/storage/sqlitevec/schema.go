package sqlitevec

const schemaSQL = `
	PRAGMA journal_mode = WAL;
	PRAGMA foreign_keys = ON;

	CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL,
		user_id TEXT NOT NULL DEFAULT '',
		question TEXT NOT NULL DEFAULT '',
		answer TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL,
		recorded_at TEXT NOT NULL DEFAULT '',
		storage_version INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
`

const memoryVectorsSchemaSQL = `
	CREATE VIRTUAL TABLE IF NOT EXISTS memory_vectors USING vec0(
		memory_id TEXT PRIMARY KEY,
		agent_id TEXT partition key,
		user_id TEXT partition key,
		storage_version INTEGER partition key,
		embedding FLOAT[4]
	);
`

const conversationSchemaSQL = `
	CREATE TABLE IF NOT EXISTS conversation_turns (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		question TEXT NOT NULL,
		answer TEXT NOT NULL,
		source TEXT NOT NULL,
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_conversation_turns_scope_created
	ON conversation_turns (user_id, agent_id, session_id, created_at DESC, id DESC);

	CREATE INDEX IF NOT EXISTS idx_conversation_turns_created
	ON conversation_turns (created_at);
`
