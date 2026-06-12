package retrieval

import (
	"context"
	"log"
	"math"
	"strings"

	"github.com/ayu-v0/agent-cortex/internal/conversation"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/memorymarkdown"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

const (
	DefaultSearchLimit = 10
	MaxSearchLimit     = 100
	defaultMarkdownDir = ".memory"
)

type ConversationReader interface {
	RecentTurns(userID, agentID, sessionID string, limit int) ([]conversation.Turn, error)
}

type VectorSearcher interface {
	SearchVectorIDs(agentID string, userID string, embedding []float32, limit int) ([]memory.VectorSearchResult, error)
}

type MetadataFinder interface {
	FindMetadataByIDs(ids []string) (map[string]memory.Metadata, error)
}

type Rewriter interface {
	Rewrite(ctx context.Context, input RewriteInput) RewriteResult
}

type RewriteInput struct {
	UserID    string
	AgentID   string
	SessionID string
	Question  string
	Turns     []conversation.Turn
}

type RewriteResult struct {
	Question string
	Metadata RewriteMetadata
}

type RewriteMetadata struct {
	Stage1Status          string
	Stage2Status          string
	FinalQuerySource      string
	Stage1Confidence      float64
	HistoryTurnsUsed      int
	AssistantContextChars int
	JSONRepaired          bool
	FallbackUsed          bool
	OriginalQuestionChars int
	ResolvedQuestionChars int
}

type Service struct {
	conversations     ConversationReader
	rewriter          Rewriter
	embedder          embedding.Embedder
	vectorSearcher    VectorSearcher
	metadataFinder    MetadataFinder
	memoryMarkdownDir string
	recentTurnLimit   int
	logger            func(string, ...any)
}

type Config struct {
	MemoryMarkdownDir string
	RecentTurnLimit   int
}

type Request struct {
	UserID    string
	AgentID   string
	SessionID string
	Question  string
	Limit     int
}

func NewService(conversations ConversationReader, rewriter Rewriter, embedder embedding.Embedder, vectorSearcher VectorSearcher, metadataFinder MetadataFinder, cfg Config) (*Service, error) {
	if conversations == nil || embedder == nil || vectorSearcher == nil || metadataFinder == nil {
		return nil, model.ErrInvalidConfig
	}
	if rewriter == nil {
		rewriter = OriginalQuestionRewriter{}
	}
	recentTurnLimit := cfg.RecentTurnLimit
	if recentTurnLimit <= 0 {
		recentTurnLimit = conversation.DefaultRecentTurnLimit
	}
	if recentTurnLimit > conversation.MaxRecentTurnLimit {
		return nil, conversation.ErrInvalidLimit
	}
	markdownDir := strings.TrimSpace(cfg.MemoryMarkdownDir)
	if markdownDir == "" {
		markdownDir = defaultMarkdownDir
	}
	return &Service{
		conversations:     conversations,
		rewriter:          rewriter,
		embedder:          embedder,
		vectorSearcher:    vectorSearcher,
		metadataFinder:    metadataFinder,
		memoryMarkdownDir: markdownDir,
		recentTurnLimit:   recentTurnLimit,
		logger:            log.Printf,
	}, nil
}

func (s *Service) SetLoggerForTest(logger func(string, ...any)) {
	if logger == nil {
		s.logger = log.Printf
		return
	}
	s.logger = logger
}

func (s *Service) Retrieve(ctx context.Context, req Request) ([]memory.SearchResult, error) {
	req.UserID = strings.TrimSpace(req.UserID)
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Question = strings.TrimSpace(req.Question)
	if err := conversation.ValidatePathID(req.UserID); err != nil {
		return nil, model.ErrInvalidRequest
	}
	if err := conversation.ValidatePathID(req.AgentID); err != nil {
		return nil, model.ErrInvalidRequest
	}
	if err := conversation.ValidateSessionID(req.SessionID); err != nil {
		return nil, model.ErrInvalidRequest
	}
	if req.Question == "" {
		return nil, embedding.ErrEmptyInput
	}

	limit, err := normalizeLimit(req.Limit)
	if err != nil {
		return nil, model.ErrInvalidRequest
	}
	turns, err := s.conversations.RecentTurns(req.UserID, req.AgentID, req.SessionID, s.recentTurnLimit)
	if err != nil {
		return nil, err
	}

	rewrite := s.rewriter.Rewrite(ctx, RewriteInput{
		UserID:    req.UserID,
		AgentID:   req.AgentID,
		SessionID: req.SessionID,
		Question:  req.Question,
		Turns:     turns,
	})
	finalQuestion := strings.TrimSpace(rewrite.Question)
	if finalQuestion == "" {
		finalQuestion = req.Question
	}
	s.logRewrite(req, rewrite, finalQuestion)

	vector, err := s.embedder.Embed(ctx, embedding.Input{Text: finalQuestion})
	if err != nil {
		return nil, err
	}

	candidates, err := s.vectorSearcher.SearchVectorIDs(req.AgentID, req.UserID, []float32(vector), overfetchLimit(limit))
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []memory.SearchResult{}, nil
	}

	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	metadata, err := s.metadataFinder.FindMetadataByIDs(ids)
	if err != nil {
		return nil, err
	}

	results := make([]memory.SearchResult, 0, limit)
	for _, candidate := range candidates {
		meta, ok := metadata[candidate.ID]
		if !ok {
			s.logger("memory retrieval skipped missing_metadata id=%q user_id=%q agent_id=%q session_id=%q", candidate.ID, req.UserID, req.AgentID, req.SessionID)
			continue
		}
		if meta.StorageVersion != memory.DailyMarkdownStorageVersion || meta.RecordedAt.IsZero() {
			s.logger("memory retrieval skipped invalid_metadata id=%q user_id=%q agent_id=%q session_id=%q", candidate.ID, req.UserID, req.AgentID, req.SessionID)
			continue
		}
		entry, ok, err := memorymarkdown.ReadEntry(s.memoryMarkdownDir, meta)
		if err != nil || !ok {
			s.logger("memory retrieval skipped markdown id=%q user_id=%q agent_id=%q session_id=%q err=%v", candidate.ID, req.UserID, req.AgentID, req.SessionID, err)
			continue
		}
		results = append(results, memory.SearchResult{
			ID:       candidate.ID,
			Content:  entry.Content,
			Distance: candidate.Distance,
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *Service) logRewrite(req Request, rewrite RewriteResult, finalQuestion string) {
	meta := rewrite.Metadata
	if meta.OriginalQuestionChars == 0 {
		meta.OriginalQuestionChars = len([]rune(req.Question))
	}
	if meta.ResolvedQuestionChars == 0 {
		meta.ResolvedQuestionChars = len([]rune(finalQuestion))
	}
	s.logger(
		"query rewrite user_id=%q agent_id=%q session_id=%q stage1_status=%q stage2_status=%q final_query_source=%q stage1_confidence=%f history_turns_used=%d assistant_context_chars=%d json_repaired=%t fallback_used=%t original_question_chars=%d resolved_question_chars=%d original_question=%q resolved_question=%q",
		req.UserID,
		req.AgentID,
		req.SessionID,
		meta.Stage1Status,
		meta.Stage2Status,
		meta.FinalQuerySource,
		meta.Stage1Confidence,
		meta.HistoryTurnsUsed,
		meta.AssistantContextChars,
		meta.JSONRepaired,
		meta.FallbackUsed,
		meta.OriginalQuestionChars,
		meta.ResolvedQuestionChars,
		req.Question,
		finalQuestion,
	)
}

func normalizeLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultSearchLimit, nil
	}
	if limit < 0 || limit > MaxSearchLimit {
		return 0, model.ErrInvalidRequest
	}
	return limit, nil
}

func overfetchLimit(limit int) int {
	return int(math.Min(float64(limit*3), MaxSearchLimit))
}

type OriginalQuestionRewriter struct{}

func (OriginalQuestionRewriter) Rewrite(_ context.Context, input RewriteInput) RewriteResult {
	return RewriteResult{
		Question: strings.TrimSpace(input.Question),
		Metadata: RewriteMetadata{
			Stage1Status:          "skipped",
			Stage2Status:          "not_run",
			FinalQuerySource:      "original",
			HistoryTurnsUsed:      len(input.Turns),
			OriginalQuestionChars: len([]rune(strings.TrimSpace(input.Question))),
			ResolvedQuestionChars: len([]rune(strings.TrimSpace(input.Question))),
		},
	}
}
