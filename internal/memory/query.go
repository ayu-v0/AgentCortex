package memory

type Query struct {
	backend Backend
}

func newQuery(backend Backend) *Query {
	return &Query{backend: backend}
}

func (q *Query) Search(agentID string, embedding []float32, limit int) ([]SearchResult, error) {
	if err := validateEmbedding(embedding); err != nil {
		return nil, err
	}

	return q.backend.Search(agentID, embedding, normalizeSearchLimit(limit))
}
