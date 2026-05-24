package memory

type Store struct {
	backend Backend
}

func newStore(backend Backend) *Store {
	return &Store{backend: backend}
}

func (s *Store) Save(memory Memory) error {
	if len(memory.Embedding) > 0 {
		if err := validateEmbedding(memory.Embedding); err != nil {
			return err
		}
	}

	return s.backend.Save(memory)
}
