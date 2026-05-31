package http

import (
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
}

func NewServer(service *memory.Service, embedder embedding.Embedder) *Server {
	return newServer(service, embedder, defaultMemoryMarkdownDir)
}

func newServer(service *memory.Service, embedder embedding.Embedder, memoryMarkdownDir string) *Server {
	handlers := newHandlers(service, embedder, memoryMarkdownDir)
	return &Server{router: newRouter(handlers)}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
