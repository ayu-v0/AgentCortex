package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultLlamaCPPTimeout = 10 * time.Second

type LlamaCPPEmbedder struct {
	endpoint   string
	model      string
	dimensions int
	apiKey     string
	client     *http.Client
}

var _ Embedder = (*LlamaCPPEmbedder)(nil)

func NewLlamaCPPEmbedder(config Config) (*LlamaCPPEmbedder, error) {
	if config.Dimensions <= 0 {
		return nil, fmt.Errorf("%w: dimensions must be positive", ErrInvalidConfig)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("%w: endpoint is required", ErrInvalidConfig)
	}

	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = DefaultLlamaCPPModel
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultLlamaCPPTimeout
	}

	return &LlamaCPPEmbedder{
		endpoint:   endpoint,
		model:      model,
		dimensions: config.Dimensions,
		apiKey:     strings.TrimSpace(config.APIKey),
		client:     &http.Client{Timeout: timeout},
	}, nil
}

func (e *LlamaCPPEmbedder) Embed(ctx context.Context, input Input) (Vector, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, ErrEmptyInput
	}

	body, err := json.Marshal(llamaCPPEmbeddingRequest{
		Model: e.model,
		Input: text,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode request: %v", ErrProviderUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", ErrProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%w: request failed: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}

	var decoded llamaCPPEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrProviderUnavailable, err)
	}
	if len(decoded.Data) == 0 || len(decoded.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("%w: response missing embedding", ErrProviderUnavailable)
	}

	vector := Vector(decoded.Data[0].Embedding)
	if err := vector.Validate(e.dimensions); err != nil {
		return nil, err
	}
	return vector, nil
}

type llamaCPPEmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type llamaCPPEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}
