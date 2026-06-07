package http

import (
	stdhttp "net/http"

	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
}

func NewServer(service *memory.Service, embedder embedding.Embedder, streamer model.Streamer) *Server {
	return newServer(service, embedder, streamer, defaultMemoryMarkdownDir)
}

func newServer(service *memory.Service, embedder embedding.Embedder, streamer model.Streamer, memoryMarkdownDir string) *Server {
	handlers := newHandlers(service, embedder, streamer, memoryMarkdownDir)
	return &Server{router: newRouter(handlers)}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *Server) Handler() stdhttp.Handler {
	return s.router
}
