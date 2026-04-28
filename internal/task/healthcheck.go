package task

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

// ChannelProbeTask 周期性向非 ALIVE 渠道发送一发最小请求，让健康但流量稀少
// 的渠道在审计统计中尽快脱离 NEW/FLAKY 状态。
//
// 与手动 ChannelTest 的关键区别：
//   - ChannelTest 是诊断工具，不写 StatsChannel；
//   - ChannelProbeTask 是“合成流量”，**会**写 StatsChannel，
//     和真实请求共用同一统计口径，因此 auditChannels.py 脚本能据此
//     判定渠道质量带。
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

// isChannelAlive 复刻 scripts/auditChannels.py 的 ALIVE 阈值，
// 避免重复探测稳定渠道。
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

	req := buildChannelTestRequest(channel.Type, modelName)
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
		if m != "" {
			return m
		}
	}
	return ""
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
