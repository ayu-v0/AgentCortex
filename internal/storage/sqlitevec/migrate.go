package sqlitevec

func (b *Backend) migrate() error {
	if _, err := b.db.Exec(schemaSQL); err != nil {
		return err
	}
	return b.ensureMemoryColumns()
}

func (b *Backend) ensureMemoryColumns() error {
	columns := map[string]string{
		"user_id":  "TEXT NOT NULL DEFAULT ''",
		"question": "TEXT NOT NULL DEFAULT ''",
		"answer":   "TEXT NOT NULL DEFAULT ''",
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
