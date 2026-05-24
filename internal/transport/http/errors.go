package http

import (
	"errors"
	"log"
	stdhttp "net/http"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

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
		errors.Is(err, memory.ErrInvalidEmbeddingValue):
		return stdhttp.StatusBadRequest
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
