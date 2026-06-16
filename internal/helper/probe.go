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
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	tmodel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

const (
	probeTimeout           = 10 * time.Second
	probeMaxBodyBytes      = 32 * 1024
	globalProbeConcurrency = 8
)

// ProbeResult is the in-memory result of a single probe.
type ProbeResult struct {
	ChannelID  int
	KeyID      int
	ModelName  string
	OK         bool
	StatusCode int
	LatencyMs  int64
	LastError  string
	ErrorClass model.ErrorClass
}

// ToStatus converts a ProbeResult to a persistable ChannelKeyModelStatus.
func (r ProbeResult) ToStatus() model.ChannelKeyModelStatus {
	return model.ChannelKeyModelStatus{
		ChannelID:    r.ChannelID,
		KeyID:        r.KeyID,
		ModelName:    r.ModelName,
		OK:           r.OK,
		StatusCode:   r.StatusCode,
		LatencyMs:    r.LatencyMs,
		LastError:    r.LastError,
		ErrorClass:   r.ErrorClass,
		LastTestedAt: time.Now().Unix(),
	}
}

// ProbeKeyModel runs a single (key, model) probe against a channel.
// It builds a minimal chat or embedding request and dispatches it via the
// channel's outbound transformer + http client.
func ProbeKeyModel(ctx context.Context, channel *model.Channel, key model.ChannelKey, modelName string) ProbeResult {
	res := ProbeResult{
		ChannelID: channel.ID,
		KeyID:     key.ID,
		ModelName: modelName,
	}

	httpClient, err := ChannelHttpClient(channel)
	if err != nil {
		res.LastError = err.Error()
		res.ErrorClass = model.ErrorClassOther
		return res
	}

	outAdapter := outbound.Get(channel.Type)
	if outAdapter == nil {
		res.LastError = fmt.Sprintf("unsupported channel type: %d", channel.Type)
		res.ErrorClass = model.ErrorClassOther
		return res
	}

	isEmbedding := outbound.IsEmbeddingChannelType(channel.Type)
	isChat := outbound.IsChatChannelType(channel.Type)
	if !isEmbedding && !isChat {
		res.LastError = fmt.Sprintf("unsupported channel type: %d", channel.Type)
		res.ErrorClass = model.ErrorClassOther
		return res
	}

	var req *tmodel.InternalLLMRequest
	if isEmbedding {
		single := "hi"
		req = &tmodel.InternalLLMRequest{
			Model:          modelName,
			EmbeddingInput: &tmodel.EmbeddingInput{Single: &single},
		}
	} else {
		var maxTokens int64 = 1
		content := "hi"
		req = &tmodel.InternalLLMRequest{
			Model: modelName,
			Messages: []tmodel.Message{
				{
					Role:    "user",
					Content: tmodel.MessageContent{Content: &content},
				},
			},
			MaxTokens: &maxTokens,
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	baseUrl := channel.GetBaseUrl()
	if baseUrl == "" {
		res.LastError = "no base url configured"
		res.ErrorClass = model.ErrorClassOther
		return res
	}

	httpReq, err := outAdapter.TransformRequest(probeCtx, req, baseUrl, key.ChannelKey)
	if err != nil {
		res.LastError = err.Error()
		res.ErrorClass = model.ErrorClassOther
		return res
	}

	for _, h := range channel.CustomHeader {
		if h.HeaderKey != "" {
			httpReq.Header.Set(h.HeaderKey, h.HeaderValue)
		}
	}

	start := time.Now()
	resp, err := httpClient.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		res.LatencyMs = latency.Milliseconds()
		res.LastError = err.Error()
		res.ErrorClass = ClassifyError(0, err)
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode
	res.LatencyMs = latency.Milliseconds()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, probeMaxBodyBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(bodyBytes))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		res.LastError = truncate(msg, 512)
		res.ErrorClass = ClassifyError(resp.StatusCode, errors.New(msg))
		return res
	}

	if isEmbedding {
		var probe struct {
			Data []json.RawMessage `json:"data"`
		}
		if jerr := json.Unmarshal(bodyBytes, &probe); jerr != nil || len(probe.Data) == 0 {
			res.LastError = truncateErr(jerr, bodyBytes, "no embedding data")
			res.ErrorClass = ClassifyError(resp.StatusCode, errors.New("non-embedding response"))
			return res
		}
	} else {
		bufResp := &http.Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
		}
		internal, terr := outAdapter.TransformResponse(probeCtx, bufResp)
		if terr != nil {
			res.LastError = truncateErr(terr, bodyBytes, "")
			res.ErrorClass = ClassifyError(resp.StatusCode, terr)
			return res
		}
		if internal == nil || (!internal.IsChatResponse() && !internal.IsEmbeddingResponse()) {
			res.LastError = truncate(string(bodyBytes), 256)
			res.ErrorClass = model.ErrorClassUpstreamError
			return res
		}
	}

	res.OK = true
	return res
}

func truncateErr(err error, body []byte, fallback string) string {
	if err != nil {
		return err.Error()
	}
	s := strings.TrimSpace(string(body))
	if s == "" {
		return fallback
	}
	return truncate(s, 256)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ChannelModels returns deduped non-empty model names for a channel based on
// Model + CustomModel.
func ChannelModels(channel *model.Channel) []string {
	if channel == nil {
		return nil
	}
	all := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
	if len(all) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(all))
	out := make([]string, 0, len(all))
	for _, m := range all {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// ProbeChannel probes every (enabled key × model) for a single channel using
// the supplied global semaphore. Disabled keys and empty model lists are
// skipped. Pass nil for the semaphore to run unbounded.
func ProbeChannel(ctx context.Context, channel *model.Channel, sem chan struct{}) []ProbeResult {
	if channel == nil || !channel.Enabled {
		return nil
	}
	models := ChannelModels(channel)
	if len(models) == 0 {
		return nil
	}
	keys := make([]model.ChannelKey, 0, len(channel.Keys))
	for _, k := range channel.Keys {
		if k.Enabled && strings.TrimSpace(k.ChannelKey) != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}

	type job struct {
		key   model.ChannelKey
		model string
	}
	jobs := make([]job, 0, len(keys)*len(models))
	for _, k := range keys {
		for _, m := range models {
			jobs = append(jobs, job{key: k, model: m})
		}
	}

	results := make([]ProbeResult, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		if sem != nil {
			sem <- struct{}{}
		}
		go func(i int, j job) {
			defer wg.Done()
			defer func() {
				if sem != nil {
					<-sem
				}
			}()
			results[i] = ProbeKeyModel(ctx, channel, j.key, j.model)
		}(i, j)
	}
	wg.Wait()

	log.Infof("probed channel %d: %d combos", channel.ID, len(results))
	return results
}

// NewProbeSem returns a fresh semaphore with the global concurrency limit.
func NewProbeSem() chan struct{} {
	return make(chan struct{}, globalProbeConcurrency)
}
