package http

import (
	stdhttp "net/http"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

type handlers struct {
	memoryService *memory.Service
}

func newHandlers(service *memory.Service) *handlers {
	return &handlers{memoryService: service}
}

func (h *handlers) health(c *gin.Context) {
	writeJSON(c, stdhttp.StatusOK, healthResponse{Status: "ok"})
}

func (h *handlers) createMemory(c *gin.Context) {
	var req createMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}

	if err := h.memoryService.Save(req.toMemory()); err != nil {
		writeHTTPError(c, err)
		return
	}

	writeJSON(c, stdhttp.StatusCreated, createMemoryResponse{ID: req.ID})
}

func (h *handlers) searchMemory(c *gin.Context) {
	var req searchMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}

	results, err := h.memoryService.Search(req.AgentID, req.Embedding, req.Limit)
	if err != nil {
		writeHTTPError(c, err)
		return
	}

	writeJSON(c, stdhttp.StatusOK, searchMemoryResponse{Results: results})
}
