package http

import (
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
}

func NewServer(service *memory.Service) *Server {
	handlers := newHandlers(service)
	return &Server{router: newRouter(handlers)}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
