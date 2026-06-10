package http

import (
	"errors"
	"log"
	stdhttp "net/http"

	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/gin-gonic/gin"
)

var ErrMemoryMarkdown = errors.New("memory markdown error")

func writeHTTPError(c *gin.Context, err error) {
	status := statusFromError(err)
	if status == stdhttp.StatusInternalServerError {
		log.Printf("store error: %v", err)
	}

	writeErrorJSON(c, status, publicMessageFromError(err))
}

func statusFromError(err error) int {
	switch {
	case errors.Is(err, memory.ErrInvalidEmbedding),
		errors.Is(err, memory.ErrInvalidEmbeddingValue),
		errors.Is(err, embedding.ErrEmptyInput),
		errors.Is(err, embedding.ErrInvalidVector),
		errors.Is(err, embedding.ErrInvalidVectorValue),
		errors.Is(err, model.ErrInvalidRequest),
		errors.Is(err, model.ErrEmptyMessages):
		return stdhttp.StatusBadRequest
	case errors.Is(err, memory.ErrMemoryConflict):
		return stdhttp.StatusConflict
	default:
		return stdhttp.StatusInternalServerError
	}
}

func publicMessageFromError(err error) string {
	if statusFromError(err) == stdhttp.StatusInternalServerError {
		return "internal server error"
	}
	return err.Error()
}
