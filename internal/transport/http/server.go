package http

import (
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
}

func NewServer(service *memory.Service) *Server {
	return newServer(service, defaultMemoryMarkdownDir)
}

func newServer(service *memory.Service, memoryMarkdownDir string) *Server {
	handlers := newHandlers(service, memoryMarkdownDir)
	return &Server{router: newRouter(handlers)}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
