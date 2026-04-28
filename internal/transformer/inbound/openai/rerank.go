package openai

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// RerankInbound implements the OpenAI/Cohere/Jina-style /v1/rerank API
// (POST { model, query, documents, top_n?, return_documents? }).
type RerankInbound struct {
	storedResponse *model.InternalLLMResponse
}

type openaiRerankRequest struct {
	Model            string             `json:"model"`
	Query            string             `json:"query"`
	Documents        []model.RerankDoc  `json:"documents"`
	TopN             *int64             `json:"top_n,omitempty"`
	ReturnDocuments  *bool              `json:"return_documents,omitempty"`
	// rank_fields and other passthrough knobs land here.
	Extra map[string]json.RawMessage `json:"-"`
}

type openaiRerankResponse struct {
	ID      string               `json:"id,omitempty"`
	Object  string               `json:"object,omitempty"`
	Model   string               `json:"model"`
	Results []model.RerankResult `json:"results"`
	Usage   *model.Usage         `json:"usage,omitempty"`
}

func (i *RerankInbound) TransformRequest(ctx context.Context, body []byte) (*model.InternalLLMRequest, error) {
	var raw openaiRerankRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	// Capture unknown top-level fields so provider-specific knobs (e.g.
	// Cohere's rank_fields) survive the round-trip without us having to
	// enumerate them here.
	var extras map[string]any
	if err := json.Unmarshal(body, &extras); err == nil {
		known := map[string]struct{}{
			"model":            {},
			"query":            {},
			"documents":        {},
			"top_n":            {},
			"return_documents": {},
		}
		for k := range extras {
			if _, ok := known[k]; ok {
				delete(extras, k)
			}
		}
	}

	req := &model.InternalLLMRequest{
		Model: raw.Model,
		RerankInput: &model.RerankInput{
			Query:     raw.Query,
			Documents: raw.Documents,
			Extra:     extras,
		},
		RerankTopN:            raw.TopN,
		RerankReturnDocuments: raw.ReturnDocuments,
		RawAPIFormat:          model.APIFormatOpenAIRerank,
	}
	return req, nil
}

func (i *RerankInbound) TransformResponse(ctx context.Context, response *model.InternalLLMResponse) ([]byte, error) {
	i.storedResponse = response
	out := openaiRerankResponse{
		ID:      response.ID,
		Object:  "rerank.result",
		Model:   response.Model,
		Results: response.RerankResults,
		Usage:   response.Usage,
	}
	return json.Marshal(out)
}

func (i *RerankInbound) TransformStream(ctx context.Context, stream *model.InternalLLMResponse) ([]byte, error) {
	return nil, errors.New("streaming is not supported for rerank API")
}

func (i *RerankInbound) GetInternalResponse(ctx context.Context) (*model.InternalLLMResponse, error) {
	return i.storedResponse, nil
}
