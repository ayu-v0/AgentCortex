package sqlitevec

func (b *Backend) migrate() error {
	_, err := b.db.Exec(schemaSQL)
	return err
}
