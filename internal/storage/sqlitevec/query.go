package sqlitevec

import (
	"database/sql"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/memory"
)

type Query struct {
	db *sql.DB
}

func newQuery(db *sql.DB) *Query {
	return &Query{db: db}
}

func (q *Query) Search(agentID string, userID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	rows, err := q.db.Query(`
		SELECT
			m.id,
			m.recorded_at,
			v.distance
		FROM memory_vectors v
		JOIN memories m ON m.id = v.memory_id
		WHERE v.embedding MATCH ?
		  AND k = ?
		  AND v.agent_id = ?
		  AND v.user_id = ?
		  AND v.storage_version = ?
		ORDER BY v.distance
	`, float32VectorToBytes(embedding), limit, agentID, userID, memory.DailyMarkdownStorageVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]memory.SearchResult, 0)
	for rows.Next() {
		var (
			result     memory.SearchResult
			recordedAt string
		)
		if err := rows.Scan(&result.ID, &recordedAt, &result.Distance); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339, recordedAt)
		if err != nil {
			return nil, err
		}
		result.RecordedAt = parsed.UTC()
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func (q *Query) SearchVectorIDs(agentID string, userID string, embedding []float32, limit int) ([]memory.VectorSearchResult, error) {
	rows, err := q.db.Query(`
		SELECT
			v.memory_id,
			v.distance
		FROM memory_vectors v
		WHERE v.embedding MATCH ?
		  AND k = ?
		  AND v.agent_id = ?
		  AND v.user_id = ?
		  AND v.storage_version = ?
		ORDER BY v.distance
	`, float32VectorToBytes(embedding), limit, agentID, userID, memory.DailyMarkdownStorageVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]memory.VectorSearchResult, 0)
	for rows.Next() {
		var result memory.VectorSearchResult
		if err := rows.Scan(&result.ID, &result.Distance); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
