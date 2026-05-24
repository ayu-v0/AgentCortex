package http

import (
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

type healthResponse struct {
	Status string `json:"status"`
}

type createMemoryResponse struct {
	ID string `json:"id"`
}

type searchMemoryResponse struct {
	Results []memory.SearchResult `json:"results"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(c *gin.Context, status int, body any) {
	c.JSON(status, body)
}

func writeErrorJSON(c *gin.Context, status int, message string) {
	writeJSON(c, status, errorResponse{Error: message})
}
