package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// RerankOutbound posts to the upstream's /rerank endpoint using the
// Cohere/Jina/Voyage-compatible JSON shape and converts the response
// back into our internal representation.
type RerankOutbound struct{}

type upstreamRerankResponse struct {
	ID      string               `json:"id"`
	Model   string               `json:"model"`
	Results []model.RerankResult `json:"results"`
	Usage   *model.Usage         `json:"usage,omitempty"`
}

func (o *RerankOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if !request.IsRerankRequest() {
		return nil, errors.New("not a rerank request")
	}

	payload := map[string]any{
		"model":     request.Model,
		"query":     request.RerankInput.Query,
		"documents": request.RerankInput.Documents,
	}
	if request.RerankTopN != nil {
		payload["top_n"] = *request.RerankTopN
	}
	if request.RerankReturnDocuments != nil {
		payload["return_documents"] = *request.RerankReturnDocuments
	}
	for k, v := range request.RerankInput.Extra {
		// Don't let client-supplied extras shadow protocol fields.
		if _, exists := payload[k]; exists {
			continue
		}
		payload[k] = v
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/rerank"
	req.URL = parsedUrl
	req.Method = http.MethodPost
	return req, nil
}

func (o *RerankOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read rerank response: %w", err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("rerank response body is empty")
	}
	var up upstreamRerankResponse
	if err := json.Unmarshal(body, &up); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rerank response: %w", err)
	}
	return &model.InternalLLMResponse{
		ID:            up.ID,
		Object:        "rerank.result",
		Model:         up.Model,
		RerankResults: up.Results,
		Usage:         up.Usage,
	}, nil
}

func (o *RerankOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	return nil, errors.New("streaming is not supported for rerank API")
}
