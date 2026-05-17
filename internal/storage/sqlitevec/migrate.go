package sqlitevec

func (s *Store) migrate() error {
	_, err := s.db.Exec(schemaSQL)
	return err
}
