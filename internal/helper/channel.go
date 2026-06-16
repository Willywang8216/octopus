package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
	"github.com/dlclark/regexp2"
)

func ChannelHttpClient(channel *model.Channel) (*http.Client, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	if !channel.Proxy {
		return client.GetHTTPClientSystemProxy(false)
	} else if channel.ChannelProxy == nil || strings.TrimSpace(*channel.ChannelProxy) == "" {
		return client.GetHTTPClientSystemProxy(true)
	} else {
		return client.GetHTTPClientCustomProxy(strings.TrimSpace(*channel.ChannelProxy))
	}
}

func ChannelBaseUrlDelayUpdate(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	newBaseUrls := make([]model.BaseUrl, 0, len(channel.BaseUrls))
	for _, baseUrl := range channel.BaseUrls {
		if baseUrl.URL == "" {
			continue
		}
		httpClient, err := ChannelHttpClient(channel)
		if err != nil {
			log.Warnf("failed to get http client (channel=%d): %v", channel.ID, err)
			continue
		}
		delay, err := GetUrlDelay(httpClient, baseUrl.URL, ctx)
		if err != nil {
			log.Warnf("failed to get url delay (channel=%d): %v", channel.ID, err)
			continue
		}
		newBaseUrls = append(newBaseUrls, model.BaseUrl{
			URL:   baseUrl.URL,
			Delay: delay,
		})
	}
	if len(newBaseUrls) > 0 {
		op.ChannelBaseUrlUpdate(channel.ID, newBaseUrls)
	}
}

func groupLooksLikeEmbeddingGroup(group model.Group) bool {
	name := strings.ToLower(strings.TrimSpace(group.Name))
	if strings.Contains(name, "embed") {
		return true
	}
	re := strings.ToLower(strings.TrimSpace(group.MatchRegex))
	if strings.Contains(re, "embed") {
		return true
	}
	return false
}

func ChannelAutoGroup(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	if channel.AutoGroup == model.AutoGroupTypeNone {
		return
	}
	groups, err := op.GroupList(ctx)
	if err != nil {
		log.Warnf("get group list failed: %v", err)
		return
	}

	channelModelNames := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
	if len(channelModelNames) == 0 {
		return
	}

	isEmbeddingChannel := outbound.IsEmbeddingChannelType(channel.Type)

	for _, group := range groups {
		// Avoid creating obviously-unroutable items (e.g. embedding groups pointing to chat channels).
		if groupLooksLikeEmbeddingGroup(group) && !isEmbeddingChannel {
			continue
		}

		matchedModelNames := make([]string, 0, len(channelModelNames))

		switch channel.AutoGroup {
		case model.AutoGroupTypeExact:
			for _, modelName := range channelModelNames {
				if strings.EqualFold(modelName, group.Name) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}

		case model.AutoGroupTypeFuzzy:
			groupNameLower := strings.ToLower(strings.TrimSpace(group.Name))
			if groupNameLower == "" {
				continue
			}
			for _, modelName := range channelModelNames {
				if strings.Contains(strings.ToLower(modelName), groupNameLower) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}

		case model.AutoGroupTypeRegex:
			if group.MatchRegex == "" {
				for _, modelName := range channelModelNames {
					if strings.EqualFold(modelName, group.Name) {
						matchedModelNames = append(matchedModelNames, modelName)
					}
				}
				break
			}

			re, err := regexp2.Compile(group.MatchRegex, regexp2.ECMAScript)
			if err != nil {
				log.Warnf("compile regex failed (channel=%d group=%d regex=%q): %v", channel.ID, group.ID, group.MatchRegex, err)
				continue
			}
			for _, modelName := range channelModelNames {
				matched, err := re.MatchString(modelName)
				if err != nil {
					log.Warnf("match regex failed (channel=%d group=%d regex=%q model=%q): %v", channel.ID, group.ID, group.MatchRegex, modelName, err)
					continue
				}
				if matched {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		}

		if len(matchedModelNames) > 0 {
			items := make([]model.GroupIDAndLLMName, 0, len(matchedModelNames))
			for _, modelName := range matchedModelNames {
				items = append(items, model.GroupIDAndLLMName{
					ChannelID: channel.ID,
					ModelName: modelName,
				})
			}
			if err := op.GroupItemBatchAdd(group.ID, items, ctx); err != nil {
				log.Warnf("group item batch add failed (channel=%d group=%d): %v", channel.ID, group.ID, err)
			}
		}
	}
}

// TestModelAvailability tests whether a specific model is actually usable on
// a channel by sending a meaningful (non-suspicious) chat completion request.
// Returns true if the model responds successfully, false otherwise.
func TestModelAvailability(ctx context.Context, channel model.Channel, modelName string) (bool, error) {
	httpClient, err := ChannelHttpClient(&channel)
	if err != nil {
		return false, err
	}

	baseURL := channel.GetBaseUrl()
	if baseURL == "" {
		return false, errors.New("no base URL available")
	}
	key := channel.GetChannelKey()
	if key.ChannelKey == "" {
		return false, errors.New("no API key available")
	}

	// Use a meaningful, benign prompt that won't trigger abuse detection.
	payload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": "What is the result of 15 multiplied by 7? Reply with only the number."},
		},
		"max_tokens": 10,
		"stream":     false,
	}

	body, _ := json.Marshal(payload)

	var endpoint string
	switch channel.Type {
	case outbound.OutboundTypeAnthropic:
		endpoint = baseURL + "/messages"
	default:
		endpoint = baseURL + "/chat/completions"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}

	req.Header.Set("Content-Type", "application/json")

	switch channel.Type {
	case outbound.OutboundTypeAnthropic:
		req.Header.Set("X-Api-Key", key.ChannelKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")
	case outbound.OutboundTypeGemini:
		req.Header.Set("X-Goog-Api-Key", key.ChannelKey)
	default:
		req.Header.Set("Authorization", "Bearer "+key.ChannelKey)
	}

	for _, h := range channel.CustomHeader {
		if h.HeaderKey != "" {
			req.Header.Set(h.HeaderKey, h.HeaderValue)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	return false, fmt.Errorf("model %s returned status %d", modelName, resp.StatusCode)
}
