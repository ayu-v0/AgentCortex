package memory

type Memory struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchResult struct {
	ID       string  `json:"id"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}
