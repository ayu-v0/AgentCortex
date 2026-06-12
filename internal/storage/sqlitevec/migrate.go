package sqlitevec

func (b *Backend) migrate() error {
	if _, err := b.db.Exec(schemaSQL); err != nil {
		return err
	}
	if err := b.ensureMemoryColumns(); err != nil {
		return err
	}
	if err := b.ensureMemoryVectorSchema(); err != nil {
		return err
	}
	_, err := b.db.Exec(conversationSchemaSQL)
	return err
}

func (b *Backend) ensureMemoryColumns() error {
	columns := map[string]string{
		"user_id":         "TEXT NOT NULL DEFAULT ''",
		"question":        "TEXT NOT NULL DEFAULT ''",
		"answer":          "TEXT NOT NULL DEFAULT ''",
		"recorded_at":     "TEXT NOT NULL DEFAULT ''",
		"storage_version": "INTEGER NOT NULL DEFAULT 0",
	}

	for name, definition := range columns {
		exists, err := b.memoryColumnExists(name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := b.db.Exec("ALTER TABLE memories ADD COLUMN " + name + " " + definition); err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) memoryColumnExists(name string) (bool, error) {
	rows, err := b.db.Query("PRAGMA table_info(memories)")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			columnName string
			columnType string
			notNull    int
			defaultVal any
			primaryKey int
		)
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, err
		}
		if columnName == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (b *Backend) ensureMemoryVectorSchema() error {
	hasAgentID, hasUserID, hasStorageVersion, err := b.memoryVectorPartitionColumnsExist()
	if err != nil {
		return err
	}
	if hasAgentID && hasUserID && hasStorageVersion {
		return nil
	}

	exists, err := b.memoryVectorTableExists()
	if err != nil {
		return err
	}
	if err := b.stageMemoryVectorsForMigration(exists); err != nil {
		return err
	}

	// vec0 tables cannot be ALTERed to add partition keys.
	if _, err := b.db.Exec("DROP TABLE IF EXISTS memory_vectors"); err != nil {
		return err
	}
	if _, err := b.db.Exec(memoryVectorsSchemaSQL); err != nil {
		return err
	}
	if err := b.restoreStagedMemoryVectors(); err != nil {
		return err
	}
	_, err = b.db.Exec("DROP TABLE IF EXISTS temp.memory_vectors_migration")
	return err
}

func (b *Backend) memoryVectorPartitionColumnsExist() (bool, bool, bool, error) {
	rows, err := b.db.Query("PRAGMA table_info(memory_vectors)")
	if err != nil {
		return false, false, false, err
	}
	defer rows.Close()

	hasAgentID := false
	hasUserID := false
	hasStorageVersion := false
	for rows.Next() {
		var (
			cid        int
			columnName string
			columnType string
			notNull    int
			defaultVal any
			primaryKey int
		)
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, false, false, err
		}
		switch columnName {
		case "agent_id":
			hasAgentID = true
		case "user_id":
			hasUserID = true
		case "storage_version":
			hasStorageVersion = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, false, false, err
	}
	return hasAgentID, hasUserID, hasStorageVersion, nil
}

func (b *Backend) memoryVectorTableExists() (bool, error) {
	var count int
	if err := b.db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
		  AND name = 'memory_vectors'
	`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (b *Backend) stageMemoryVectorsForMigration(tableExists bool) error {
	if _, err := b.db.Exec("DROP TABLE IF EXISTS temp.memory_vectors_migration"); err != nil {
		return err
	}
	if _, err := b.db.Exec(`
		CREATE TEMP TABLE memory_vectors_migration (
			memory_id TEXT PRIMARY KEY,
			embedding BLOB NOT NULL
		)
	`); err != nil {
		return err
	}
	if !tableExists {
		return nil
	}
	_, err := b.db.Exec(`
		INSERT INTO temp.memory_vectors_migration (memory_id, embedding)
		SELECT memory_id, embedding
		FROM memory_vectors
	`)
	return err
}

func (b *Backend) restoreStagedMemoryVectors() error {
	_, err := b.db.Exec(`
		INSERT INTO memory_vectors (memory_id, agent_id, user_id, storage_version, embedding)
		SELECT s.memory_id, m.agent_id, m.user_id, m.storage_version, s.embedding
		FROM temp.memory_vectors_migration s
		JOIN memories m ON m.id = s.memory_id
	`)
	return err
}
