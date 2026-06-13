package memory

import "time"

const DailyMarkdownStorageVersion = 1

type Memory struct {
	ID             string    `json:"id"`
	AgentID        string    `json:"agent_id"`
	UserID         string    `json:"user_id,omitempty"`
	Question       string    `json:"question,omitempty"`
	Answer         string    `json:"answer,omitempty"`
	Content        string    `json:"content,omitempty"`
	Embedding      []float32 `json:"embedding,omitempty"`
	RecordedAt     time.Time `json:"-"`
	StorageVersion int       `json:"-"`
}

type SearchResult struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	Distance   float64   `json:"distance"`
	RecordedAt time.Time `json:"-"`
}

func SameContent(left, right Memory) bool {
	return left.ID == right.ID &&
		left.AgentID == right.AgentID &&
		left.UserID == right.UserID &&
		left.Question == right.Question &&
		left.Answer == right.Answer
}
