package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
	"github.com/tmaxmax/go-sse"
)

// nonStreamAttemptTimeout caps the wall time of a single non-streaming relay
// attempt. If exceeded, the iterator advances to the next channel. Streaming
// requests are NOT bounded by this timeout — they rely on group.FirstTokenTimeOut
// for first-token detection and the underlying transport's ResponseHeaderTimeout
// for connection-level health.
const nonStreamAttemptTimeout = 90 * time.Second

// Handler 处理入站请求并转发到上游服务
func Handler(inboundType inbound.InboundType, c *gin.Context) {
	internalRequest, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}
	if supportedModels := c.GetString("supported_models"); supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, internalRequest.Model) {
			resp.Error(c, http.StatusBadRequest, "model not supported")
			return
		}
	}

	requestModel := internalRequest.Model
	apiKeyID := c.GetInt("api_key_id")

	group, err := op.GroupGetEnabledMap(requestModel, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "model not found")
		return
	}

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.Error(c, http.StatusServiceUnavailable, "no available channel")
		return
	}

	metrics := NewRelayMetrics(apiKeyID, requestModel, internalRequest)

	req := &relayRequest{
		c:               c,
		inAdapter:       inAdapter,
		internalRequest: internalRequest,
		metrics:         metrics,
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
		iter:            iter,
	}

	var lastErr error

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Infof("request context canceled, stopping retry")
			metrics.Save(c.Request.Context(), false, context.Canceled, iter.Attempts())
			return
		default:
		}

		item := iter.Item()

		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}

		// 粘性命中时优先沿用上次成功的 key，确保 (channel, key) 元组对会话保持有意义。
		usedKey := channel.GetChannelKeyByID(iter.StickyKeyID())
		if usedKey.ChannelKey == "" {
			iter.Skip(channel.ID, 0, channel.Name, "no available key")
			continue
		}
		keyID := keys[0].ID

		outAdapter := outbound.Get(channel.Type)
		if outAdapter == nil {
			iter.Skip(channel.ID, keyID, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		attemptRequest := *internalRequest
		attemptRequest.Model = item.ModelName
		if channel.ParamOverride != nil && strings.TrimSpace(*channel.ParamOverride) != "" {
			if err := json.Unmarshal([]byte(*channel.ParamOverride), &attemptRequest); err != nil {
				log.Warnf("failed to apply param override for channel %s: %v", channel.Name, err)
			}
			attemptRequest.Model = item.ModelName
		}

		if attemptRequest.IsEmbeddingRequest() && !outbound.IsEmbeddingChannelType(channel.Type) {
			iter.Skip(channel.ID, keyID, channel.Name, "channel type not compatible with embedding request")
			continue
		}
		if internalRequest.IsRerankRequest() && !outbound.IsRerankChannelType(channel.Type) {
			iter.Skip(channel.ID, usedKey.ID, channel.Name, "channel type not compatible with rerank request")
			continue
		}
		if internalRequest.IsRerankRequest() && !outbound.IsRerankChannelType(channel.Type) {
			iter.Skip(channel.ID, usedKey.ID, channel.Name, "channel type not compatible with rerank request")
			continue
		}
		if internalRequest.IsChatRequest() && !outbound.IsChatChannelType(channel.Type) {
			iter.Skip(channel.ID, usedKey.ID, channel.Name, "channel type not compatible with chat request")
			continue
		}

		log.Infof("request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)", requestModel, group.Mode, channel.Name, item.ModelName, iter.Index()+1, iter.Len(), iter.IsSticky())

		for _, usedKey := range keys {
			if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				continue
			}
			ra := &relayAttempt{
				relayRequest:         req,
				outAdapter:           outAdapter,
				channel:              channel,
				usedKey:              usedKey,
				attemptRequest:       &attemptRequest,
				groupItemID:          item.ID,
				firstTokenTimeOutSec: group.FirstTokenTimeOut,
			}

			result := ra.attempt()
			if result.Success {
				metrics.Save(c.Request.Context(), true, nil, iter.Attempts())
				return
			}
			lastErr = result.Err
			if result.Written {
				metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
				return
			}
			if result.SkipChannel {
				break
			}
		}
	}

	metrics.Save(c.Request.Context(), false, lastErr, iter.Attempts())
	msg := "all channels failed"
	if lastErr != nil {
		msg = fmt.Sprintf("all channels failed: %v", lastErr)
	}
	resp.Error(c, http.StatusBadGateway, msg)
}

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name)

	// 转发请求
	statusCode, fwdErr := ra.forward()

	// Host concurrency fail-fast: treat as a skip so we can fail over immediately.
	// Do not punish keys / circuit breaker for local admission control.
	if fwdErr != nil && errors.Is(fwdErr, client.ErrHostConcurrencyLimitReached) {
		span.End(dbmodel.AttemptSkipped, statusCode, fwdErr.Error())
		// Host-level admission control has nothing to do with a specific key.
		// Trying other keys would just waste attempts; fail over to the next channel.
		return attemptResult{Success: false, Written: false, SkipChannel: true, Err: fwdErr}
	}

	// 更新 channel key 状态
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	if fwdErr == nil {
		// ====== 成功 ======
		ra.collectResponse()
		ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
		op.ChannelMarkKeySuccess(ra.usedKey)

		span.End(dbmodel.AttemptSuccess, statusCode, "")

		// Channel 维度统计
		op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.attemptRequest.Model)
		// 会话保持：更新粘性记录
		balancer.SetSticky(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)

		return attemptResult{Success: true}
	}

	// ====== 失败 ======
	billingIssue := isBillingIssue(statusCode, fwdErr)
	op.ChannelMarkKeyFailure(ra.usedKey, statusCode, fwdErr, billingIssue)
	if err := op.ChannelCheckAutoDisable(ra.channel.ID, billingIssue, ra.c.Request.Context()); err != nil {
		log.Warnf("failed to check channel auto disable (channel=%d): %v", ra.channel.ID, err)
	}
	span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())

	// Channel 维度统计
	op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})

	// 熔断器：记录失败
	balancer.RecordFailure(ra.channel.ID, ra.usedKey.ID, ra.attemptRequest.Model)

	// Detect funding/quota issues and tag the key accordingly.
	if tag := detectFundingIssue(statusCode, fwdErr.Error()); tag != "" {
		log.Warnf("funding issue detected for channel %s key %d: %s (status=%d)",
			ra.channel.Name, ra.usedKey.ID, tag, statusCode)
		_ = op.ChannelKeySetStatusTag(ra.usedKey.ID, tag, true)

		// If all keys are now disabled, auto-disable the entire channel.
		if op.ChannelAllKeysDisabled(ra.channel.ID) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = op.ChannelSetAutoDisabled(ra.channel.ID, bgCtx)
			log.Warnf("all keys exhausted for channel %s — channel auto-disabled", ra.channel.Name)
		}
	}

	written := ra.c.Writer.Written()
	if written {
		ra.collectResponse()
	}
	return attemptResult{
		Success:      false,
		Written:      written,
		BillingIssue: billingIssue,
		Err:          fmt.Errorf("channel %s failed: %v", ra.channel.Name, fwdErr),
	}
}

func isBillingIssue(statusCode int, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	billingKeywords := []string{
		"insufficient fund",
		"insufficient_fund",
		"insufficient funds",
		"not enough quota",
		"quota exceeded",
		"billing",
		"balance",
		"credit",
		"payment required",
		"no money",
		"余额不足",
		"额度不足",
		"配额不足",
		"没钱",
		"欠费",
		"upstream stream closed before first payload",
		"upstream closed before any payload",
		"empty_stream",
	}
	for _, keyword := range billingKeywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	if (statusCode == http.StatusInternalServerError || statusCode == http.StatusBadGateway) &&
		(strings.Contains(msg, "closed before") || strings.Contains(msg, "empty_stream")) {
		return true
	}
	return false
}

// parseRequest 解析并验证入站请求
func parseRequest(inboundType inbound.InboundType, c *gin.Context) (*model.InternalLLMRequest, model.Inbound, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	inAdapter := inbound.Get(inboundType)
	if inAdapter == nil {
		err := fmt.Errorf("unsupported inbound type: %d", inboundType)
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}
	internalRequest, err := inAdapter.TransformRequest(c.Request.Context(), body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	// Pass through the original query parameters
	internalRequest.Query = c.Request.URL.Query()

	if err := internalRequest.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, err
	}

	return internalRequest, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward() (int, error) {
	ctx := ra.c.Request.Context()

	// Cap non-streaming attempts so a hung upstream can't pin the request.
	// Streaming uses the first-token timeout from group config instead.
	isStream := ra.internalRequest.Stream != nil && *ra.internalRequest.Stream
	if !isStream {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, nonStreamAttemptTimeout)
		defer cancel()
	}

	// 构建出站请求
	outboundRequest, err := ra.outAdapter.TransformRequest(
		ctx,
		ra.attemptRequest,
		ra.channel.GetBaseUrl(),
		ra.usedKey.ChannelKey,
	)
	if err != nil {
		log.Warnf("failed to create request: %v", err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// 复制请求头
	ra.copyHeaders(outboundRequest)

	// 发送请求
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// 检查响应状态
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		if err != nil {
			return response.StatusCode, fmt.Errorf("failed to read response body: %w", err)
		}
		return response.StatusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	// 处理响应
	if ra.attemptRequest.Stream != nil && *ra.attemptRequest.Stream {
		if err := ra.handleStreamResponse(ctx, response); err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	if err := ra.handleResponse(ctx, response); err != nil {
		return 0, err
	}
	return response.StatusCode, nil
}

func (ra *relayAttempt) handleUpstreamFailure(statusCode int, body []byte) {
	ra.maybeAutoDisableGroupItem(statusCode, body)
	ra.maybeAutoDisableKey(statusCode, body)
}

func (ra *relayAttempt) maybeAutoDisableGroupItem(statusCode int, body []byte) {
	if ra == nil || ra.groupItemID == 0 {
		return
	}
	if statusCode != http.StatusServiceUnavailable {
		return
	}

	lower := bytes.ToLower(body)
	if !bytes.Contains(lower, []byte("\u65e0\u53ef\u7528\u6e20\u9053")) && !bytes.Contains(lower, []byte("no available channel")) {
		return
	}

	reason := strings.TrimSpace(string(body))
	if len(reason) > 256 {
		reason = reason[:256]
	}
	if err := op.GroupItemDisable(ra.groupItemID, reason, ra.c.Request.Context()); err != nil {
		log.Warnf("failed to auto-disable group item %d: %v", ra.groupItemID, err)
	}
}

func (ra *relayAttempt) maybeAutoDisableKey(statusCode int, body []byte) {
	enabled, err := op.SettingGetBool(dbmodel.SettingKeyChannelKeyAutoDisableEnabled)
	if err != nil || !enabled {
		return
	}
	if ra == nil || ra.channel == nil {
		return
	}
	if ra.usedKey.ID == 0 || ra.usedKey.ChannelID == 0 {
		return
	}
	if !ra.usedKey.Enabled {
		return
	}

	lower := bytes.ToLower(body)
	if bytes.Contains(lower, []byte("stream must be set to true")) {
		return
	}

	// Rate limit should be temporary; Channel.GetAvailableKeys() already backs off for 5 minutes.
	if statusCode == http.StatusTooManyRequests {
		return
	}

	category := ""
	shouldDisable := false

	// no_money (insufficient funds / quota)
	if bytes.Contains(lower, []byte("insufficient")) ||
		bytes.Contains(lower, []byte("balance_insufficient")) ||
		bytes.Contains(lower, []byte("insufficient_user_quota")) ||
		bytes.Contains(lower, []byte("insufficient_fund")) ||
		bytes.Contains(lower, []byte("\u4f59\u989d\u4e0d\u8db3")) ||
		bytes.Contains(lower, []byte("\u989d\u5ea6\u4e0d\u8db3")) ||
		bytes.Contains(lower, []byte("\u9884\u6263\u8d39\u989d\u5ea6\u5931\u8d25")) {
		shouldDisable = true
		category = "no_money"
	}

	// bad_gateway / temporary upstream failures
	if statusCode == http.StatusBadGateway || statusCode == http.StatusGatewayTimeout || statusCode == 520 || statusCode == 522 || statusCode == 524 {
		shouldDisable = true
		if category == "" {
			category = "bad_gateway"
		}
	}
	if bytes.Contains(lower, []byte("cloudflare")) || bytes.Contains(lower, []byte("cf-ray")) || bytes.Contains(lower, []byte("bad gateway")) {
		shouldDisable = true
		if category == "" {
			category = "bad_gateway"
		}
	}

	// invalid_key / blocked
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusPaymentRequired {
		shouldDisable = true
		if category == "" {
			category = "invalid_key"
		}
	}
	if bytes.Contains(lower, []byte("your request was block")) || bytes.Contains(lower, []byte("request was block")) || bytes.Contains(lower, []byte("request was blocked")) {
		shouldDisable = true
		if category == "" {
			category = "bad_gateway"
		}
	}

	if category == "" {
		category = "http_" + fmt.Sprintf("%d", statusCode)
	}
	if !shouldDisable {
		return
	}

	reason := strings.TrimSpace(string(body))
	if len(reason) > 256 {
		reason = reason[:256]
	}

	prevRemark := strings.TrimSpace(ra.usedKey.Remark)
	ra.usedKey.Enabled = false
	ra.usedKey.Remark = fmt.Sprintf("auto-disabled: category=%s status=%d time=%s reason=%s", category, statusCode, time.Now().UTC().Format(time.RFC3339), reason)
	if prevRemark != "" {
		ra.usedKey.Remark += " | prev=" + prevRemark
	}

	if err := op.ChannelKeyUpdate(ra.usedKey); err != nil {
		log.Warnf("failed to auto-disable channel key %d for channel %s: %v", ra.usedKey.ID, ra.channel.Name, err)
		return
	}

	ra.maybeAutoDisableChannel(category, statusCode, reason)
}

func (ra *relayAttempt) maybeAutoDisableChannel(category string, statusCode int, reason string) {
	ch, err := op.ChannelGet(ra.channel.ID, ra.c.Request.Context())
	if err != nil {
		return
	}
	if !ch.Enabled {
		return
	}

	for _, k := range ch.Keys {
		if !k.Enabled {
			continue
		}
		if strings.TrimSpace(k.ChannelKey) == "" {
			continue
		}
		return
	}

	msg := fmt.Sprintf("auto-disabled: category=%s status=%d time=%s reason=all keys disabled | last=%s",
		category, statusCode, time.Now().UTC().Format(time.RFC3339), reason)
	_ = op.ChannelAutoDisable(ch.ID, msg, ra.c.Request.Context())
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头
func (ra *relayAttempt) copyHeaders(outboundRequest *http.Request) {
	for key, values := range ra.c.Request.Header {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			outboundRequest.Header.Set(key, value)
		}
	}
	if len(ra.channel.CustomHeader) > 0 {
		for _, header := range ra.channel.CustomHeader {
			outboundRequest.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
}

// sendRequest 发送 HTTP 请求
func (ra *relayAttempt) sendRequest(req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHttpClient(ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}

	response, err := httpClient.Do(req)
	if err != nil {
		log.Warnf("failed to send request: %v", err)
		return nil, err
	}

	return response, nil
}

// handleStreamResponse 处理流式响应
func (ra *relayAttempt) handleStreamResponse(ctx context.Context, response *http.Response) error {
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// 设置 SSE 响应头
	ra.c.Header("Content-Type", "text/event-stream")
	ra.c.Header("Cache-Control", "no-cache")
	ra.c.Header("Connection", "keep-alive")
	ra.c.Header("X-Accel-Buffering", "no")

	firstToken := true

	type sseReadResult struct {
		data string
		err  error
	}
	results := make(chan sseReadResult, 1)
	go func() {
		defer close(results)
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(response.Body, readCfg) {
			if err != nil {
				results <- sseReadResult{err: err}
				return
			}
			results <- sseReadResult{data: ev.Data}
		}
	}()

	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if firstToken && ra.firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(ra.firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			log.Infof("client disconnected, stopping stream")
			return nil
		case <-firstTokenC:
			log.Warnf("first token timeout (%ds), switching channel", ra.firstTokenTimeOutSec)
			_ = response.Body.Close()
			return fmt.Errorf("first token timeout (%ds)", ra.firstTokenTimeOutSec)
		case r, ok := <-results:
			if !ok {
				log.Infof("stream end")
				return nil
			}
			if r.err != nil {
				log.Warnf("failed to read event: %v", r.err)
				return fmt.Errorf("failed to read stream event: %w", r.err)
			}

			data, err := ra.transformStreamData(ctx, r.data)
			if err != nil || len(data) == 0 {
				continue
			}
			if firstToken {
				ra.metrics.SetFirstTokenTime(time.Now())
				firstToken = false
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}

			ra.c.Writer.Write(data)
			ra.c.Writer.Flush()
		}
	}
}

// transformStreamData 转换流式数据
func (ra *relayAttempt) transformStreamData(ctx context.Context, data string) ([]byte, error) {
	internalStream, err := ra.outAdapter.TransformStream(ctx, []byte(data))
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}
	if internalStream == nil {
		return nil, nil
	}

	inStream, err := ra.inAdapter.TransformStream(ctx, internalStream)
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}

	return inStream, nil
}

// handleResponse 处理非流式响应
func (ra *relayAttempt) handleResponse(ctx context.Context, response *http.Response) error {
	internalResponse, err := ra.outAdapter.TransformResponse(ctx, response)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform outbound response: %w", err)
	}

	inResponse, err := ra.inAdapter.TransformResponse(ctx, internalResponse)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform inbound response: %w", err)
	}

	ra.c.Data(http.StatusOK, "application/json", inResponse)
	return nil
}

// collectResponse 收集响应信息
func (ra *relayAttempt) collectResponse() {
	internalResponse, err := ra.inAdapter.GetInternalResponse(ra.c.Request.Context())
	if err != nil || internalResponse == nil {
		return
	}

	ra.metrics.SetInternalResponse(internalResponse, ra.attemptRequest.Model)
}
