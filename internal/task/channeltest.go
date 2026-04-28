package task

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	tmodel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

// ChannelTestModelResult captures the outcome of probing a single
// (channel, key, model) tuple.
type ChannelTestModelResult struct {
	Model      string `json:"model"`
	Success    bool   `json:"success"`
	StatusCode int    `json:"status_code"`
	DurationMS int64  `json:"duration_ms"`
	// Error holds a short, user-facing reason when Success is false. Long
	// upstream bodies are truncated; they are useful for diagnosis without
	// flooding the UI.
	Error string `json:"error,omitempty"`
}

// ChannelTestKeyResult groups per-model results under one channel key.
type ChannelTestKeyResult struct {
	KeyID      int                      `json:"key_id"`
	KeyPreview string                   `json:"key_preview"`
	KeyRemark  string                   `json:"key_remark,omitempty"`
	Enabled    bool                     `json:"enabled"`
	Results    []ChannelTestModelResult `json:"results"`
}

// ChannelTestChannelResult is the full per-channel report.
type ChannelTestChannelResult struct {
	ChannelID    int                    `json:"channel_id"`
	ChannelName  string                 `json:"channel_name"`
	StartedAt    time.Time              `json:"started_at"`
	FinishedAt   *time.Time             `json:"finished_at,omitempty"`
	TotalModels  int                    `json:"total_models"`
	WorkedModels int                    `json:"worked_models"`
	TotalKeys    int                    `json:"total_keys"`
	WorkedKeys   int                    `json:"worked_keys"`
	Skipped      string                 `json:"skipped,omitempty"`
	Keys         []ChannelTestKeyResult `json:"keys"`
}

// ChannelTestRunStatus is the run-wide aggregate snapshot returned to the UI.
type ChannelTestRunStatus struct {
	Running           bool                              `json:"running"`
	TotalChannels     int                               `json:"total_channels"`
	CompletedChannels int                               `json:"completed_channels"`
	StartedAt         time.Time                         `json:"started_at"`
	FinishedAt        *time.Time                        `json:"finished_at,omitempty"`
	Channels          map[int]ChannelTestChannelSummary `json:"channels,omitempty"`
}

// ChannelTestChannelSummary is the lightweight per-channel block embedded in
// the run status payload for fast list rendering.
type ChannelTestChannelSummary struct {
	ChannelID    int        `json:"channel_id"`
	ChannelName  string     `json:"channel_name"`
	TotalModels  int        `json:"total_models"`
	WorkedModels int        `json:"worked_models"`
	TotalKeys    int        `json:"total_keys"`
	WorkedKeys   int        `json:"worked_keys"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Skipped      string     `json:"skipped,omitempty"`
}

// channelTestRunner holds the singleton state for the channel test feature.
// It is intentionally in-memory only; results are ephemeral and re-running
// the test discards the previous run.
type channelTestRunner struct {
	mu       sync.RWMutex
	running  bool
	cancel   context.CancelFunc
	started  time.Time
	finished *time.Time
	total    int
	done     int
	results  map[int]*ChannelTestChannelResult
}

var channelTester = &channelTestRunner{
	results: make(map[int]*ChannelTestChannelResult),
}

const (
	// channelTestPerProbeTimeout caps a single (channel, key, model) probe.
	channelTestPerProbeTimeout = 30 * time.Second
	// channelTestRunTimeout caps the entire run so a stalled channel cannot
	// pin the runner indefinitely.
	channelTestRunTimeout = 30 * time.Minute
	// channelTestMaxErrBody caps the upstream-error body we keep for the UI.
	channelTestMaxErrBody = 256
	// channelTestMaxParallel caps concurrent in-flight channels. Too many
	// parallel probes burns through provider rate limits and saturates
	// outbound bandwidth on small instances.
	channelTestMaxParallel = 4
)

// ErrChannelTestAlreadyRunning is returned by StartChannelTest when a
// previous run is still in flight.
var ErrChannelTestAlreadyRunning = errors.New("channel test already running")

// StartChannelTest kicks off a channel test run in a background goroutine.
// `channelIDs` may be empty to test every enabled channel.
func StartChannelTest(channelIDs []int) error {
	channelTester.mu.Lock()
	if channelTester.running {
		channelTester.mu.Unlock()
		return ErrChannelTestAlreadyRunning
	}

	ctx, cancel := context.WithTimeout(context.Background(), channelTestRunTimeout)
	channelTester.running = true
	channelTester.started = time.Now()
	channelTester.finished = nil
	channelTester.done = 0
	channelTester.total = 0
	channelTester.cancel = cancel
	channelTester.results = make(map[int]*ChannelTestChannelResult)
	channelTester.mu.Unlock()

	go runChannelTest(ctx, channelIDs)
	return nil
}

// CancelChannelTest aborts an in-progress run. Safe to call concurrently;
// no-op when nothing is running.
func CancelChannelTest() {
	channelTester.mu.Lock()
	defer channelTester.mu.Unlock()
	if channelTester.cancel != nil {
		channelTester.cancel()
	}
}

// ChannelTestStatus returns the current run-wide snapshot.
func ChannelTestStatus() ChannelTestRunStatus {
	channelTester.mu.RLock()
	defer channelTester.mu.RUnlock()
	summaries := make(map[int]ChannelTestChannelSummary, len(channelTester.results))
	for id, r := range channelTester.results {
		summaries[id] = ChannelTestChannelSummary{
			ChannelID:    r.ChannelID,
			ChannelName:  r.ChannelName,
			TotalModels:  r.TotalModels,
			WorkedModels: r.WorkedModels,
			TotalKeys:    r.TotalKeys,
			WorkedKeys:   r.WorkedKeys,
			StartedAt:    r.StartedAt,
			FinishedAt:   r.FinishedAt,
			Skipped:      r.Skipped,
		}
	}
	return ChannelTestRunStatus{
		Running:           channelTester.running,
		TotalChannels:     channelTester.total,
		CompletedChannels: channelTester.done,
		StartedAt:         channelTester.started,
		FinishedAt:        channelTester.finished,
		Channels:          summaries,
	}
}

// ChannelTestResult returns the full report for one channel, or nil if the
// channel hasn't been tested yet (or was never enabled at run time).
func ChannelTestResult(channelID int) *ChannelTestChannelResult {
	channelTester.mu.RLock()
	defer channelTester.mu.RUnlock()
	r, ok := channelTester.results[channelID]
	if !ok {
		return nil
	}
	clone := *r
	clone.Keys = append([]ChannelTestKeyResult(nil), r.Keys...)
	return &clone
}

// ChannelTestAllResults returns the full report for every tested channel.
func ChannelTestAllResults() map[int]*ChannelTestChannelResult {
	channelTester.mu.RLock()
	defer channelTester.mu.RUnlock()
	out := make(map[int]*ChannelTestChannelResult, len(channelTester.results))
	for id, r := range channelTester.results {
		clone := *r
		clone.Keys = append([]ChannelTestKeyResult(nil), r.Keys...)
		out[id] = &clone
	}
	return out
}

func runChannelTest(ctx context.Context, channelIDs []int) {
	defer func() {
		channelTester.mu.Lock()
		now := time.Now()
		channelTester.finished = &now
		channelTester.running = false
		channelTester.cancel = nil
		channelTester.mu.Unlock()
	}()

	channels, err := op.ChannelList(ctx)
	if err != nil {
		log.Errorf("channel test: failed to list channels: %v", err)
		return
	}

	wanted := map[int]struct{}{}
	for _, id := range channelIDs {
		wanted[id] = struct{}{}
	}

	targets := make([]model.Channel, 0, len(channels))
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if len(wanted) > 0 {
			if _, ok := wanted[ch.ID]; !ok {
				continue
			}
		}
		targets = append(targets, ch)
	}

	channelTester.mu.Lock()
	channelTester.total = len(targets)
	channelTester.mu.Unlock()

	sem := make(chan struct{}, channelTestMaxParallel)
	var wg sync.WaitGroup
	for i := range targets {
		ch := targets[i]
		wg.Add(1)
		go func(channel model.Channel) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			testChannel(ctx, &channel)
		}(ch)
	}
	wg.Wait()
}

// testChannel probes every (key, model) tuple on one channel. We snapshot
// the channel's models once at the start so concurrent edits don't change
// the test scope mid-flight.
func testChannel(ctx context.Context, channel *model.Channel) {
	startedAt := time.Now()
	result := &ChannelTestChannelResult{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		StartedAt:   startedAt,
	}
	defer func() {
		now := time.Now()
		result.FinishedAt = &now

		// Recount worked totals so the API and the UI agree even if we
		// added results out of order.
		workedModelsSet := map[string]struct{}{}
		workedKeys := 0
		for _, kr := range result.Keys {
			anyOK := false
			for _, mr := range kr.Results {
				if mr.Success {
					workedModelsSet[mr.Model] = struct{}{}
					anyOK = true
				}
			}
			if anyOK {
				workedKeys++
			}
		}
		result.WorkedModels = len(workedModelsSet)
		result.WorkedKeys = workedKeys

		channelTester.mu.Lock()
		channelTester.results[channel.ID] = result
		channelTester.done++
		channelTester.mu.Unlock()
	}()

	// Dedupe while keeping a stable, alphabetical order so the UI is
	// predictable and we don't probe the same model twice when it appears
	// in both `model` and `custom_model`.
	models := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
	uniqModels := dedupeStrings(models)
	sort.Strings(uniqModels)

	enabledKeys := make([]model.ChannelKey, 0, len(channel.Keys))
	for _, k := range channel.Keys {
		if k.Enabled && k.ChannelKey != "" {
			enabledKeys = append(enabledKeys, k)
		}
	}

	result.TotalModels = len(uniqModels)
	result.TotalKeys = len(enabledKeys)

	if len(enabledKeys) == 0 {
		result.Skipped = "no enabled keys"
		return
	}
	if len(uniqModels) == 0 {
		result.Skipped = "no models configured"
		return
	}

	outAdapter := outbound.Get(channel.Type)
	if outAdapter == nil {
		result.Skipped = "unsupported channel type"
		return
	}
	httpClient, err := helper.ChannelHttpClient(channel)
	if err != nil {
		result.Skipped = "http client init failed: " + err.Error()
		return
	}

	for _, key := range enabledKeys {
		preview := previewKey(key.ChannelKey)
		keyResult := ChannelTestKeyResult{
			KeyID:      key.ID,
			KeyPreview: preview,
			KeyRemark:  key.Remark,
			Enabled:    key.Enabled,
			Results:    make([]ChannelTestModelResult, 0, len(uniqModels)),
		}
		for _, modelName := range uniqModels {
			select {
			case <-ctx.Done():
				keyResult.Results = append(keyResult.Results, ChannelTestModelResult{
					Model:   modelName,
					Success: false,
					Error:   "test cancelled before probe started",
				})
				continue
			default:
			}
			res := probeChannelModel(ctx, channel, &key, modelName, outAdapter, httpClient)
			keyResult.Results = append(keyResult.Results, res)
		}
		result.Keys = append(result.Keys, keyResult)
	}
}

// probeChannelModel does the actual upstream call for one (key, model) pair
// and reports the structured outcome. We deliberately do not feed the result
// into StatsChannel: the test feature is a manual diagnostic, not traffic,
// so a one-off bad probe must not promote or demote a channel's quality
// classification.
func probeChannelModel(
	parent context.Context,
	channel *model.Channel,
	key *model.ChannelKey,
	modelName string,
	outAdapter tmodel.Outbound,
	httpClient *http.Client,
) ChannelTestModelResult {
	res := ChannelTestModelResult{Model: modelName}
	startedAt := time.Now()
	defer func() {
		res.DurationMS = time.Since(startedAt).Milliseconds()
	}()

	req := buildChannelTestRequest(channel.Type, modelName)
	if req == nil {
		res.Error = "no probe shape registered for this channel type"
		return res
	}

	ctx, cancel := context.WithTimeout(parent, channelTestPerProbeTimeout)
	defer cancel()

	httpReq, err := outAdapter.TransformRequest(ctx, req, channel.GetBaseUrl(), key.ChannelKey)
	if err != nil {
		res.Error = "build request: " + err.Error()
		return res
	}
	for _, h := range channel.CustomHeader {
		if h.HeaderKey != "" {
			httpReq.Header.Set(h.HeaderKey, h.HeaderValue)
		}
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode

	body, _ := io.ReadAll(io.LimitReader(resp.Body, channelTestMaxErrBody+1))
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		res.Success = true
		return res
	}

	if len(body) > channelTestMaxErrBody {
		body = body[:channelTestMaxErrBody]
	}
	res.Error = http.StatusText(resp.StatusCode)
	if len(body) > 0 {
		res.Error = res.Error + ": " + string(body)
	}
	return res
}

// buildChannelTestRequest constructs the smallest valid request shape for a
// channel type, used by the channel-test feature to verify each
// (key, model) pair without burning real tokens.
func buildChannelTestRequest(channelType outbound.OutboundType, modelName string) *tmodel.InternalLLMRequest {
	switch {
	case outbound.IsEmbeddingChannelType(channelType):
		input := "ping"
		return &tmodel.InternalLLMRequest{
			Model:          modelName,
			EmbeddingInput: &tmodel.EmbeddingInput{Single: &input},
			RawAPIFormat:   tmodel.APIFormatOpenAIEmbedding,
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

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func previewKey(k string) string {
	if len(k) <= 8 {
		return k
	}
	return k[:4] + "..." + k[len(k)-4:]
}
