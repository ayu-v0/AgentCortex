package memory

import "reflect"

type Backend interface {
	Close() error
	Save(memory Memory) error
	FindByID(id string) (Memory, bool, error)
	Search(agentID string, userID string, embedding []float32, limit int) ([]SearchResult, error)
}

type Service struct {
	backend Backend
	query   *Query
	store   *Store
}

func NewService(backend Backend) (*Service, error) {
	if isNilBackend(backend) {
		return nil, ErrNilBackend
	}

	return &Service{
		backend: backend,
		query:   newQuery(backend),
		store:   newStore(backend),
	}, nil
}

func (s *Service) Close() error {
	return s.backend.Close()
}

func (s *Service) Save(memory Memory) error {
	return s.store.Save(memory)
}

func (s *Service) FindByID(id string) (Memory, bool, error) {
	return s.backend.FindByID(id)
}

func (s *Service) Search(agentID string, userID string, embedding []float32, limit int) ([]SearchResult, error) {
	return s.query.Search(agentID, userID, embedding, limit)
}

func isNilBackend(backend Backend) bool {
	if backend == nil {
		return true
	}

	value := reflect.ValueOf(backend)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
