package helper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	tmodel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// ChannelProbeResult is the structured result of a single (channel, key, model)
// probe. The caller is responsible for persisting/aggregating it.
type ChannelProbeResult struct {
	ChannelID  int                         `json:"channel_id"`
	KeyID      int                         `json:"key_id"`
	Model      string                      `json:"model"`
	Success    bool                        `json:"success"`
	StatusCode int                         `json:"status_code"`
	LatencyMs  int                         `json:"latency_ms"`
	ErrorClass model.ChannelTestErrorClass `json:"error_class"`
	ErrorMsg   string                      `json:"error_msg"`
}

// ProbeChannelKeyModel sends a minimal chat (or embedding) request to the
// upstream provider for the given (channel, key, model) and returns a
// classified, summarised result. It does not write to the relay log or stats
// pipelines — it is purely a side-channel health check.
func ProbeChannelKeyModel(ctx context.Context, channel *model.Channel, key model.ChannelKey, modelName string, timeout time.Duration) ChannelProbeResult {
	res := ChannelProbeResult{
		ChannelID: channel.ID,
		KeyID:     key.ID,
		Model:     modelName,
	}
	if strings.TrimSpace(modelName) == "" {
		res.ErrorClass = model.ChannelTestErrorBadRequest
		res.ErrorMsg = "empty model name"
		return res
	}
	if strings.TrimSpace(key.ChannelKey) == "" {
		res.ErrorClass = model.ChannelTestErrorAuth
		res.ErrorMsg = "empty api key"
		return res
	}
	baseURL := channel.GetBaseUrl()
	if baseURL == "" {
		res.ErrorClass = model.ChannelTestErrorTransform
		res.ErrorMsg = "channel has no base url configured"
		return res
	}

	out := outbound.Get(channel.Type)
	if out == nil {
		res.ErrorClass = model.ChannelTestErrorUnsupported
		res.ErrorMsg = fmt.Sprintf("unsupported channel type %d", channel.Type)
		return res
	}

	probeReq := buildProbeRequest(channel.Type, modelName)
	if err := probeReq.Validate(); err != nil {
		res.ErrorClass = model.ChannelTestErrorTransform
		res.ErrorMsg = fmt.Sprintf("probe request invalid: %v", err)
		return res
	}

	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := out.TransformRequest(probeCtx, probeReq, baseURL, key.ChannelKey)
	if err != nil {
		res.ErrorClass = model.ChannelTestErrorTransform
		res.ErrorMsg = fmt.Sprintf("transform request failed: %v", err)
		return res
	}
	for _, h := range channel.CustomHeader {
		if h.HeaderKey != "" {
			httpReq.Header.Set(h.HeaderKey, h.HeaderValue)
		}
	}

	httpClient, err := ChannelHttpClient(channel)
	if err != nil {
		res.ErrorClass = model.ChannelTestErrorNetwork
		res.ErrorMsg = fmt.Sprintf("http client error: %v", err)
		return res
	}

	start := time.Now()
	resp, err := httpClient.Do(httpReq)
	res.LatencyMs = int(time.Since(start).Milliseconds())
	if err != nil {
		res.ErrorClass = classifyTransportError(err)
		res.ErrorMsg = trimMessage(err.Error(), 512)
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	bodyStr := string(body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// 2xx is "the upstream accepted the request and started replying" —
		// for a probe this is sufficient evidence that the (key, model)
		// works.
		res.Success = true
		return res
	}

	res.ErrorClass = classifyHTTPError(resp.StatusCode, bodyStr)
	res.ErrorMsg = trimMessage(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, bodyStr), 512)
	return res
}

// buildProbeRequest constructs a minimal valid InternalLLMRequest for the
// channel type. Embedding channels need an EmbeddingInput, chat channels
// need at least one user message.
func buildProbeRequest(channelType outbound.OutboundType, modelName string) *tmodel.InternalLLMRequest {
	if outbound.IsEmbeddingChannelType(channelType) {
		probe := "ping"
		input := tmodel.EmbeddingInput{Single: &probe}
		return &tmodel.InternalLLMRequest{
			Model:          modelName,
			EmbeddingInput: &input,
		}
	}

	hi := "ping"
	maxTokens := int64(1)
	stream := false
	return &tmodel.InternalLLMRequest{
		Model: modelName,
		Messages: []tmodel.Message{
			{
				Role: "user",
				Content: tmodel.MessageContent{
					Content: &hi,
				},
			},
		},
		MaxTokens:           &maxTokens,
		MaxCompletionTokens: &maxTokens,
		Stream:              &stream,
	}
}

// classifyHTTPError maps an upstream HTTP error to a stable error class so
// the frontend can render the right "attention needed" tag and the
// auto-disable logic knows whether the failure is the user's fault
// (auth/quota) or the provider's (rate limit / 5xx).
func classifyHTTPError(status int, body string) model.ChannelTestErrorClass {
	lower := strings.ToLower(body)

	// Quota / billing patterns first — these can come back as 400/401/403/429
	// depending on the provider, so we keyword-match before status-matching.
	quotaSignals := []string{
		"insufficient_quota",
		"insufficient quota",
		"insufficient balance",
		"insufficient_funds",
		"insufficient funds",
		"insufficient credit",
		"out of credit",
		"credit_exhausted",
		"billing_hard_limit_reached",
		"billing not active",
		"quota exceeded",
		"exceeded your current quota",
		"payment required",
		"账户余额不足",
		"额度不足",
		"配额不足",
		"余额不足",
	}
	for _, sig := range quotaSignals {
		if strings.Contains(lower, sig) {
			return model.ChannelTestErrorQuota
		}
	}

	authSignals := []string{
		"invalid_api_key",
		"invalid api key",
		"incorrect api key",
		"unauthorized",
		"authentication failed",
		"invalid authorization",
		"invalid_token",
		"key not found",
		"api key not valid",
	}
	for _, sig := range authSignals {
		if strings.Contains(lower, sig) {
			return model.ChannelTestErrorAuth
		}
	}

	notFoundSignals := []string{
		"model_not_found",
		"model not found",
		"does not exist",
		"unknown model",
	}
	for _, sig := range notFoundSignals {
		if strings.Contains(lower, sig) {
			return model.ChannelTestErrorNotFound
		}
	}

	switch {
	case status == http.StatusUnauthorized:
		return model.ChannelTestErrorAuth
	case status == http.StatusPaymentRequired:
		return model.ChannelTestErrorQuota
	case status == http.StatusForbidden:
		return model.ChannelTestErrorPermission
	case status == http.StatusNotFound:
		return model.ChannelTestErrorNotFound
	case status == http.StatusTooManyRequests:
		return model.ChannelTestErrorRateLimit
	case status >= 500:
		return model.ChannelTestErrorServer
	case status >= 400:
		return model.ChannelTestErrorBadRequest
	}
	return model.ChannelTestErrorOther
}

// classifyTransportError handles the case where we never got an HTTP
// response (DNS, connection refused, TLS, proxy, timeouts, …).
func classifyTransportError(err error) model.ChannelTestErrorClass {
	if err == nil {
		return model.ChannelTestErrorNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.ChannelTestErrorTimeout
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return model.ChannelTestErrorTimeout
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return model.ChannelTestErrorTimeout
	}
	return model.ChannelTestErrorNetwork
}

func trimMessage(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
