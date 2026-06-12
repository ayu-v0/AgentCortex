package http

import (
	stdhttp "net/http"

	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/ayu-v0/agent-cortex/internal/retrieval"
	"github.com/gin-gonic/gin"
)

type Server struct {
	router   *gin.Engine
	handlers *handlers
}

func NewServer(service *memory.Service, embedder embedding.Embedder, streamer model.Streamer) *Server {
	return newServer(service, embedder, streamer, nil, nil, defaultMemoryMarkdownDir)
}

func NewServerWithServices(service *memory.Service, embedder embedding.Embedder, streamer model.Streamer, retrievalService *retrieval.Service, conversationRecorder conversationRecorder) *Server {
	return newServer(service, embedder, streamer, retrievalService, conversationRecorder, defaultMemoryMarkdownDir)
}

func newServer(service *memory.Service, embedder embedding.Embedder, streamer model.Streamer, retrievalService *retrieval.Service, conversationRecorder conversationRecorder, memoryMarkdownDir string) *Server {
	handlers := newHandlers(service, embedder, streamer, retrievalService, conversationRecorder, memoryMarkdownDir)
	return &Server{
		router:   newRouter(handlers),
		handlers: handlers,
	}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *Server) Handler() stdhttp.Handler {
	return s.router
}
