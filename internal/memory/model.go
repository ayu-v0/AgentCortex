package memory

type Memory struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	UserID    string    `json:"user_id,omitempty"`
	Question  string    `json:"question,omitempty"`
	Answer    string    `json:"answer,omitempty"`
	Content   string    `json:"content,omitempty"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchResult struct {
	ID       string  `json:"id"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}
