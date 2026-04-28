package task

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	tmodel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

// ChannelProbeTask 周期性向非 ALIVE 渠道发送一发最小请求，
// 让健康但流量稀少的渠道在审计统计中尽快脱离 NEW/FLAKY 状态。
//
// 设计要点：
//   - 仅探测 enabled = true 的渠道；DEAD/ZOMBIE 已在审计阶段被禁用。
//   - 已稳定（>=20% 成功率且 >=50 次请求）的渠道视为 ALIVE，跳过节省额度。
//   - 探测请求绕过 group/iterator，直接走 outbound transformer，结果通过
//     StatsChannelUpdate 累计，与正常流量共用同一统计口径。
//   - 单次探测带 30s 超时；整个任务运行设全局超时避免长尾阻塞。
func ChannelProbeTask() {
	log.Debugf("channel probe task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("channel probe task finished in %s", time.Since(startTime))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	channels, err := op.ChannelList(ctx)
	if err != nil {
		log.Errorf("channel probe: failed to list channels: %v", err)
		return
	}

	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		if isChannelAlive(channel.ID) {
			continue
		}
		probeChannel(ctx, &channel)
	}
}

// isChannelAlive 复刻 scripts/auditChannels.py 的 ALIVE 阈值，避免重复探测稳定渠道。
func isChannelAlive(channelID int) bool {
	stats := op.StatsChannelGet(channelID)
	total := stats.RequestSuccess + stats.RequestFailed
	if total < 50 {
		return false
	}
	return float64(stats.RequestSuccess)/float64(total) >= 0.20
}

func probeChannel(parent context.Context, channel *model.Channel) {
	modelName := pickProbeModel(channel)
	if modelName == "" {
		return
	}
	usedKey := channel.GetChannelKey()
	if usedKey.ChannelKey == "" {
		return
	}

	outAdapter := outbound.Get(channel.Type)
	if outAdapter == nil {
		return
	}

	req := buildProbeRequest(channel.Type, modelName)
	if req == nil {
		return
	}

	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	httpReq, err := outAdapter.TransformRequest(ctx, req, channel.GetBaseUrl(), usedKey.ChannelKey)
	if err != nil {
		log.Debugf("channel probe %d: build request: %v", channel.ID, err)
		recordProbeResult(channel.ID, false)
		return
	}
	for _, h := range channel.CustomHeader {
		if h.HeaderKey != "" {
			httpReq.Header.Set(h.HeaderKey, h.HeaderValue)
		}
	}

	httpClient, err := helper.ChannelHttpClient(channel)
	if err != nil {
		log.Debugf("channel probe %d: http client: %v", channel.ID, err)
		return
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.Debugf("channel probe %d: do: %v", channel.ID, err)
		recordProbeResult(channel.ID, false)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<14))

	success := resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices
	recordProbeResult(channel.ID, success)
}

func pickProbeModel(channel *model.Channel) string {
	models := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m != "" {
			return m
		}
	}
	return ""
}

func buildProbeRequest(channelType outbound.OutboundType, modelName string) *tmodel.InternalLLMRequest {
	switch {
	case outbound.IsEmbeddingChannelType(channelType):
		input := "ping"
		return &tmodel.InternalLLMRequest{
			Model:          modelName,
			EmbeddingInput: &tmodel.EmbeddingInput{Single: &input},
			RawAPIFormat:   tmodel.APIFormatOpenAIEmbedding,
		}
	case outbound.IsRerankChannelType(channelType):
		return &tmodel.InternalLLMRequest{
			Model: modelName,
			RerankInput: &tmodel.RerankInput{
				Query:     "ping",
				Documents: []tmodel.RerankDoc{{Text: "pong"}},
			},
			RawAPIFormat: tmodel.APIFormatOpenAIRerank,
		}
	case outbound.IsChatChannelType(channelType):
		ping := "ping"
		max := int64(1)
		return &tmodel.InternalLLMRequest{
			Model: modelName,
			Messages: []tmodel.Message{{
				Role:    "user",
				Content: tmodel.MessageContent{Content: &ping},
			}},
			MaxTokens:    &max,
			RawAPIFormat: tmodel.APIFormatOpenAIChatCompletion,
		}
	default:
		return nil
	}
}

func recordProbeResult(channelID int, success bool) {
	metrics := model.StatsMetrics{}
	if success {
		metrics.RequestSuccess = 1
	} else {
		metrics.RequestFailed = 1
	}
	if err := op.StatsChannelUpdate(channelID, metrics); err != nil {
		log.Debugf("channel probe %d: stats update: %v", channelID, err)
	}
}
