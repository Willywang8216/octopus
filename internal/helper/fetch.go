package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/dlclark/regexp2"
)

func FetchModels(ctx context.Context, request model.Channel) ([]string, error) {
	client, err := ChannelHttpClient(&request)
	if err != nil {
		return nil, err
	}
	fetchModel := make([]string, 0)
	switch request.Type {
	case outbound.OutboundTypeAnthropic:
		fetchModel, err = fetchAnthropicModels(client, ctx, request)
	case outbound.OutboundTypeGemini:
		fetchModel, err = fetchGeminiModels(client, ctx, request)
	default:
		fetchModel, err = fetchOpenAIModels(client, ctx, request)
	}
	if err != nil {
		return nil, err
	}
	if request.MatchRegex != nil && *request.MatchRegex != "" {
		matchModel := make([]string, 0)
		re, err := regexp2.Compile(*request.MatchRegex, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		for _, model := range fetchModel {
			matched, err := re.MatchString(model)
			if err != nil {
				return nil, err
			}
			if matched {
				matchModel = append(matchModel, model)
			}
		}
		return matchModel, nil
	}
	return fetchModel, nil
}

func FetchAvailableModels(ctx context.Context, request model.Channel) ([]string, error) {
	models, err := FetchModels(ctx, request)
	if err != nil {
		return nil, err
	}
	availableModels := make([]string, 0, len(models))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := CheckModelAvailability(checkCtx, request, modelName)
		cancel()
		if err != nil {
			continue
		}
		availableModels = append(availableModels, modelName)
	}
	return availableModels, nil
}

func CheckModelAvailability(ctx context.Context, request model.Channel, modelName string) error {
	client, err := ChannelHttpClient(&request)
	if err != nil {
		return err
	}
	switch request.Type {
	case outbound.OutboundTypeGemini:
		return checkGeminiModelAvailability(client, ctx, request, modelName)
	case outbound.OutboundTypeAnthropic:
		return checkAnthropicModelAvailability(client, ctx, request, modelName)
	default:
		return checkOpenAIModelAvailability(client, ctx, request, modelName)
	}
}

func checkOpenAIModelAvailability(client *http.Client, ctx context.Context, request model.Channel, modelName string) error {
	body := map[string]any{
		"model":       modelName,
		"temperature": 0,
		"max_tokens":  8,
		"messages": []map[string]string{
			{"role": "system", "content": "Answer with only the result of the arithmetic expression. This is a normal model availability check."},
			{"role": "user", "content": "Compute 17 + 25."},
		},
	}
	return postAvailabilityCheck(client, ctx, request, request.GetBaseUrl()+"/chat/completions", "Bearer "+request.GetChannelKey().ChannelKey, body)
}

func checkAnthropicModelAvailability(client *http.Client, ctx context.Context, request model.Channel, modelName string) error {
	body := map[string]any{
		"model":      modelName,
		"max_tokens": 8,
		"messages": []map[string]string{
			{"role": "user", "content": "Compute 17 + 25. Answer with only the number."},
		},
	}
	return postAvailabilityCheck(client, ctx, request, request.GetBaseUrl()+"/messages", request.GetChannelKey().ChannelKey, body)
}

func checkGeminiModelAvailability(client *http.Client, ctx context.Context, request model.Channel, modelName string) error {
	body := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": "Compute 17 + 25. Answer with only the number."},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":     0,
			"maxOutputTokens": 8,
		},
	}
	return postAvailabilityCheck(client, ctx, request, request.GetBaseUrl()+"/models/"+modelName+":generateContent", request.GetChannelKey().ChannelKey, body)
}

func postAvailabilityCheck(client *http.Client, ctx context.Context, request model.Channel, endpoint, authValue string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	switch request.Type {
	case outbound.OutboundTypeAnthropic:
		req.Header.Set("X-Api-Key", authValue)
		req.Header.Set("Anthropic-Version", "2023-06-01")
	case outbound.OutboundTypeGemini:
		req.Header.Set("X-Goog-Api-Key", authValue)
	default:
		req.Header.Set("Authorization", authValue)
	}
	for _, header := range request.CustomHeader {
		if header.HeaderKey != "" {
			req.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return fmt.Errorf("availability check failed: %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// refer: https://platform.openai.com/docs/api-reference/models/list
func fetchOpenAIModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		request.GetBaseUrl()+"/models",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+request.GetChannelKey().ChannelKey)
	for _, header := range request.CustomHeader {
		if header.HeaderKey != "" {
			req.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result model.OpenAIModelList

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

// refer: https://ai.google.dev/api/models
func fetchGeminiModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	var allModels []string
	pageToken := ""

	for {
		req, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			request.GetBaseUrl()+"/models",
			nil,
		)
		req.Header.Set("X-Goog-Api-Key", request.GetChannelKey().ChannelKey)
		for _, header := range request.CustomHeader {
			if header.HeaderKey != "" {
				req.Header.Set(header.HeaderKey, header.HeaderValue)
			}
		}
		if pageToken != "" {
			q := req.URL.Query()
			q.Add("pageToken", pageToken)
			req.URL.RawQuery = q.Encode()
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var result model.GeminiModelList

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		for _, m := range result.Models {
			name := strings.TrimPrefix(m.Name, "models/")
			allModels = append(allModels, name)
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	if len(allModels) == 0 {
		return fetchOpenAIModels(client, ctx, request)
	}
	return allModels, nil
}

// refer: https://platform.claude.com/docs
func fetchAnthropicModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {

	var allModels []string
	var afterID string
	for {

		req, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			request.GetBaseUrl()+"/models",
			nil,
		)
		req.Header.Set("X-Api-Key", request.GetChannelKey().ChannelKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")
		for _, header := range request.CustomHeader {
			if header.HeaderKey != "" {
				req.Header.Set(header.HeaderKey, header.HeaderValue)
			}
		}
		// 设置多页参数
		q := req.URL.Query()

		if afterID != "" {
			q.Set("after_id", afterID)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var result model.AnthropicModelList

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		for _, m := range result.Data {
			allModels = append(allModels, m.ID)
		}

		if !result.HasMore {
			break
		}

		afterID = result.LastID
	}
	if len(allModels) == 0 {
		return fetchOpenAIModels(client, ctx, request)
	}
	return allModels, nil
}
