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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS memory_vectors USING vec0(
		memory_id TEXT PRIMARY KEY,
		embedding FLOAT[4]
	);
`
