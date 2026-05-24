package sqlitevec

import (
	"database/sql"

	"github.com/ayu-v0/agent-cortex/internal/memory"
)

type Query struct {
	db *sql.DB
}

func newQuery(db *sql.DB) *Query {
	return &Query{db: db}
}

func (q *Query) Search(agentID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	rows, err := q.db.Query(`
		SELECT
			m.id,
			m.content,
			v.distance
		FROM memory_vectors v
		JOIN memories m ON m.id = v.memory_id
		WHERE v.embedding MATCH ?
		  AND k = ?
		  AND m.agent_id = ?
		ORDER BY v.distance
	`, float32VectorToBytes(embedding), limit, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]memory.SearchResult, 0)
	for rows.Next() {
		var result memory.SearchResult
		if err := rows.Scan(&result.ID, &result.Content, &result.Distance); err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
