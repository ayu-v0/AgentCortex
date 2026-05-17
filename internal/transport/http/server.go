package http

import (
	"errors"
	"log"
	stdhttp "net/http"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
}

type createMemoryRequest struct {
	ID        string    `json:"id" binding:"required"`
	AgentID   string    `json:"agent_id" binding:"required"`
	Content   string    `json:"content" binding:"required"`
	Embedding []float32 `json:"embedding" binding:"required"`
}

type searchMemoryRequest struct {
	AgentID   string    `json:"agent_id" binding:"required"`
	Embedding []float32 `json:"embedding" binding:"required"`
	Limit     int       `json:"limit" binding:"omitempty,min=1,max=100"`
}

func NewServer(store *memory.Store) *Server {
	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(stdhttp.StatusOK, gin.H{"status": "ok"})
	})

	api := router.Group("/api/v1")
	api.POST("/memories", createMemoryHandler(store))
	api.POST("/memories/search", searchMemoryHandler(store))

	return &Server{router: router}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func createMemoryHandler(store *memory.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createMemoryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(stdhttp.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err := store.Save(memory.Memory{
			ID:        req.ID,
			AgentID:   req.AgentID,
			Content:   req.Content,
			Embedding: req.Embedding,
		})
		if err != nil {
			writeStoreError(c, err)
			return
		}

		c.JSON(stdhttp.StatusCreated, gin.H{"id": req.ID})
	}
}

func searchMemoryHandler(store *memory.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req searchMemoryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(stdhttp.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		results, err := store.Search(req.AgentID, req.Embedding, req.Limit)
		if err != nil {
			writeStoreError(c, err)
			return
		}

		c.JSON(stdhttp.StatusOK, gin.H{"results": results})
	}
}

func writeStoreError(c *gin.Context, err error) {
	if errors.Is(err, memory.ErrInvalidEmbedding) || errors.Is(err, memory.ErrInvalidEmbeddingValue) {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("store error: %v", err)
	c.JSON(stdhttp.StatusInternalServerError, gin.H{"error": "internal server error"})
}
