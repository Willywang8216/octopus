package task

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

// ChannelTestSummary is the per-channel response returned to the UI after a
// channel-level test run completes.
type ChannelTestSummary struct {
	ChannelID    int                       `json:"channel_id"`
	ChannelName  string                    `json:"channel_name"`
	TotalKeys    int                       `json:"total_keys"`
	TotalModels  int                       `json:"total_models"`
	TotalProbes  int                       `json:"total_probes"`
	SuccessCount int                       `json:"success_count"`
	FailCount    int                       `json:"fail_count"`
	DurationMs   int                       `json:"duration_ms"`
	Keys         []ChannelTestKeySummary   `json:"keys"`
	TestedAt     int64                     `json:"tested_at"`
	Disabled     *ChannelDisabledTagDetail `json:"disabled,omitempty"`
	Running      bool                      `json:"running,omitempty"`
}

// ChannelTestKeySummary describes a single key's results within a channel
// test run.
type ChannelTestKeySummary struct {
	KeyID          int                         `json:"key_id"`
	KeyPreview     string                      `json:"key_preview"`
	Remark         string                      `json:"remark"`
	Enabled        bool                        `json:"enabled"`
	AutoDisabled   bool                        `json:"auto_disabled"`
	DisabledReason string                      `json:"disabled_reason"`
	DisabledClass  model.ChannelTestErrorClass `json:"disabled_class"`
	SuccessCount   int                         `json:"success_count"`
	FailCount      int                         `json:"fail_count"`
	Models         []model.ChannelTestResult   `json:"models"`
}

// ChannelDisabledTagDetail is what the UI uses to render the destructive
// "attention needed" tag on a channel that was auto-disabled (or had every
// key auto-disabled).
type ChannelDisabledTagDetail struct {
	AutoDisabled bool                        `json:"auto_disabled"`
	Reason       string                      `json:"disabled_reason"`
	Class        model.ChannelTestErrorClass `json:"disabled_class"`
	DisabledAt   int64                       `json:"disabled_at"`
}

// ChannelTestProgress is an in-memory live view of a channel probe run. It is
// intentionally not persisted; final per-model results remain in
// ChannelTestResult. The UI polls this while a manual or scheduled probe is in
// flight so users can see which key/model is active instead of staring at
// "no result".
type ChannelTestProgress struct {
	ChannelID       int    `json:"channel_id"`
	ChannelName     string `json:"channel_name"`
	Running         bool   `json:"running"`
	Phase           string `json:"phase"`
	CurrentKeyID    int    `json:"current_key_id"`
	CurrentKey      string `json:"current_key"`
	CurrentModel    string `json:"current_model"`
	TotalKeys       int    `json:"total_keys"`
	TotalModels     int    `json:"total_models"`
	TotalProbes     int    `json:"total_probes"`
	CompletedProbes int    `json:"completed_probes"`
	SuccessCount    int    `json:"success_count"`
	FailCount       int    `json:"fail_count"`
	StartedAt       int64  `json:"started_at"`
	UpdatedAt       int64  `json:"updated_at"`
	FinishedAt      int64  `json:"finished_at"`
	LastError       string `json:"last_error"`
}

var channelTestProgress sync.Map // channelID -> ChannelTestProgress
var channelTestProgressMu sync.Mutex

func ChannelTestProgressGet(channelID int) *ChannelTestProgress {
	channelTestProgressMu.Lock()
	defer channelTestProgressMu.Unlock()
	v, ok := channelTestProgress.Load(channelID)
	if !ok {
		return nil
	}
	progress := v.(ChannelTestProgress)
	return &progress
}

func channelTestProgressStore(progress ChannelTestProgress) {
	channelTestProgressMu.Lock()
	defer channelTestProgressMu.Unlock()
	progress.UpdatedAt = time.Now().Unix()
	channelTestProgress.Store(progress.ChannelID, progress)
}

func channelTestProgressPatch(channelID int, patch func(*ChannelTestProgress)) {
	channelTestProgressMu.Lock()
	defer channelTestProgressMu.Unlock()
	v, ok := channelTestProgress.Load(channelID)
	if !ok {
		return
	}
	progress := v.(ChannelTestProgress)
	patch(&progress)
	progress.UpdatedAt = time.Now().Unix()
	channelTestProgress.Store(progress.ChannelID, progress)
}

// channelTestMu serialises test runs for a single channel so concurrent test
// requests don't fight over the same channel's keys.
var channelTestMu sync.Map // channelID -> *sync.Mutex

func channelTestLock(channelID int) *sync.Mutex {
	if v, ok := channelTestMu.Load(channelID); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := channelTestMu.LoadOrStore(channelID, mu)
	return actual.(*sync.Mutex)
}

// ChannelTestRun probes every (key, model) combination for the channel and
// returns a structured summary. When forceAllKeys is true it ignores the
// Enabled flag on a key (used by the periodic re-test of auto-disabled
// keys). When modelFilter is non-empty, only those models are tested.
func ChannelTestRun(ctx context.Context, channelID int, modelFilter []string, forceAllKeys bool) (*ChannelTestSummary, error) {
	startedAt := time.Now().Unix()
	channelTestProgressStore(ChannelTestProgress{
		ChannelID: channelID,
		Running:   true,
		Phase:     "waiting",
		StartedAt: startedAt,
	})

	mu := channelTestLock(channelID)
	mu.Lock()
	defer mu.Unlock()

	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		channelTestProgressPatch(channelID, func(p *ChannelTestProgress) {
			p.Running = false
			p.Phase = "failed"
			p.FinishedAt = time.Now().Unix()
			p.LastError = err.Error()
		})
		return nil, err
	}

	keys := channel.Keys
	if !forceAllKeys {
		keys = filterKeys(keys, false)
	}
	if len(keys) == 0 {
		err := fmt.Errorf("channel has no keys to test")
		channelTestProgressPatch(channelID, func(p *ChannelTestProgress) {
			p.ChannelName = channel.Name
			p.Running = false
			p.Phase = "failed"
			p.FinishedAt = time.Now().Unix()
			p.LastError = err.Error()
		})
		return nil, err
	}

	models := channelModels(channel)
	if len(modelFilter) > 0 {
		models = intersectModels(models, modelFilter)
	}
	if len(models) == 0 {
		err := fmt.Errorf("channel has no models to test")
		channelTestProgressPatch(channelID, func(p *ChannelTestProgress) {
			p.ChannelName = channel.Name
			p.Running = false
			p.Phase = "failed"
			p.FinishedAt = time.Now().Unix()
			p.LastError = err.Error()
		})
		return nil, err
	}

	timeout := probeTimeout()
	start := time.Now()
	totalProbes := len(keys) * len(models)
	channelTestProgressStore(ChannelTestProgress{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		Running:     true,
		Phase:       "running",
		TotalKeys:   len(keys),
		TotalModels: len(models),
		TotalProbes: totalProbes,
		StartedAt:   startedAt,
	})

	// Run probes with bounded concurrency. A small pool keeps total request
	// load manageable when a channel has many keys × models.
	const concurrency = 4
	type job struct {
		key model.ChannelKey
		mdl string
	}
	jobs := make(chan job, totalProbes)
	results := make(chan helper.ChannelProbeResult, totalProbes)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				channelTestProgressPatch(channel.ID, func(p *ChannelTestProgress) {
					p.Phase = "running"
					p.CurrentKeyID = j.key.ID
					p.CurrentKey = previewKeyProbe(j.key.ChannelKey)
					p.CurrentModel = j.mdl
				})
				result := helper.ProbeChannelKeyModel(ctx, channel, j.key, j.mdl, timeout)
				channelTestProgressPatch(channel.ID, func(p *ChannelTestProgress) {
					p.CompletedProbes++
					if result.Success {
						p.SuccessCount++
					} else {
						p.FailCount++
					}
				})
				results <- result
			}
		}()
	}
	for _, k := range keys {
		for _, m := range models {
			jobs <- job{key: k, mdl: m}
		}
	}
	close(jobs)
	wg.Wait()
	close(results)

	now := time.Now().Unix()
	keyResults := make(map[int][]model.ChannelTestResult, len(keys))
	for r := range results {
		keyResults[r.KeyID] = append(keyResults[r.KeyID], model.ChannelTestResult{
			ChannelID:  r.ChannelID,
			KeyID:      r.KeyID,
			Model:      r.Model,
			Success:    r.Success,
			StatusCode: r.StatusCode,
			LatencyMs:  r.LatencyMs,
			ErrorClass:  r.ErrorClass,
			ErrorMsg:    r.ErrorMsg,
			ResponseLog: r.ResponseLog,
			TestedAt:    now,
		})
	}

	summary := &ChannelTestSummary{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		TotalKeys:   len(keys),
		TotalModels: len(models),
		TotalProbes: totalProbes,
		TestedAt:    now,
	}

	channelTestProgressPatch(channel.ID, func(p *ChannelTestProgress) {
		p.Phase = "saving"
		p.CurrentKeyID = 0
		p.CurrentKey = ""
		p.CurrentModel = ""
	})
	if err := persistTestRun(ctx, channel, keys, models, keyResults, now, summary); err != nil {
		channelTestProgressPatch(channel.ID, func(p *ChannelTestProgress) {
			p.Running = false
			p.Phase = "failed"
			p.FinishedAt = time.Now().Unix()
			p.LastError = err.Error()
		})
		return nil, err
	}
	summary.DurationMs = int(time.Since(start).Milliseconds())

	if err := op.ChannelRefreshCacheByID(channel.ID, ctx); err != nil {
		log.Warnf("failed to refresh channel %d cache after test: %v", channel.ID, err)
	}

	if fresh, err := op.ChannelGet(channel.ID, ctx); err == nil && (fresh.AutoDisabled || (!fresh.Enabled && fresh.DisabledReason != "")) {
		summary.Disabled = &ChannelDisabledTagDetail{
			AutoDisabled: fresh.AutoDisabled,
			Reason:       fresh.DisabledReason,
			Class:        fresh.DisabledClass,
			DisabledAt:   fresh.DisabledAt,
		}
	}

	channelTestProgressPatch(channel.ID, func(p *ChannelTestProgress) {
		p.Running = false
		p.Phase = "done"
		p.FinishedAt = time.Now().Unix()
		p.SuccessCount = summary.SuccessCount
		p.FailCount = summary.FailCount
		p.CompletedProbes = summary.SuccessCount + summary.FailCount
	})

	return summary, nil
}

// persistTestRun writes the test results, updates per-key counters, and runs
// the auto-disable / auto-enable logic in a single transaction.
func persistTestRun(
	ctx context.Context,
	channel *model.Channel,
	testedKeys []model.ChannelKey,
	testedModels []string,
	keyResults map[int][]model.ChannelTestResult,
	now int64,
	summary *ChannelTestSummary,
) error {
	tx := db.GetDB().WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	channelTotalSuccess := 0
	channelTotalFail := 0

	keySummaries := make([]ChannelTestKeySummary, 0, len(testedKeys))

	for _, k := range testedKeys {
		modelResults := keyResults[k.ID]
		if len(testedModels) > 0 {
			if err := tx.Where("channel_id = ? AND key_id = ? AND model IN ?", channel.ID, k.ID, testedModels).
				Delete(&model.ChannelTestResult{}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("delete prior test results: %w", err)
			}
		}
		if len(modelResults) > 0 {
			if err := tx.Create(&modelResults).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("insert test results: %w", err)
			}
		}

		successCount := 0
		failCount := 0
		var worstReason string
		var worstClass model.ChannelTestErrorClass
		for _, r := range modelResults {
			if r.Success {
				successCount++
				continue
			}
			failCount++
			if r.ErrorClass.IsAttentionNeeded() && worstReason == "" {
				worstReason = r.ErrorMsg
				worstClass = r.ErrorClass
			}
		}
		channelTotalSuccess += successCount
		channelTotalFail += failCount

		keyEnabled := k.Enabled
		keyAutoDisabled := k.AutoDisabled
		keyDisabledReason := k.DisabledReason
		keyDisabledClass := k.DisabledClass
		keyDisabledAt := k.DisabledAt

		switch {
		case successCount > 0:
			if k.AutoDisabled {
				keyEnabled = true
				keyAutoDisabled = false
				keyDisabledReason = ""
				keyDisabledClass = ""
				keyDisabledAt = 0
				log.Infof("auto-re-enabled key %d on channel %s (probe success)", k.ID, channel.Name)
			}
		case failCount > 0 && worstClass != "":
			if keyEnabled || !keyAutoDisabled {
				log.Warnf("auto-disabling key %d on channel %s: %s (%s)", k.ID, channel.Name, worstClass, worstReason)
			}
			keyEnabled = false
			keyAutoDisabled = true
			keyDisabledClass = worstClass
			keyDisabledReason = humanReason(worstClass, worstReason)
			if keyDisabledAt == 0 {
				keyDisabledAt = now
			}
		}

		updateMap := map[string]interface{}{
			"last_test_at":      now,
			"last_test_success": successCount,
			"last_test_failed":  failCount,
			"enabled":           keyEnabled,
			"auto_disabled":     keyAutoDisabled,
			"disabled_reason":   keyDisabledReason,
			"disabled_class":    keyDisabledClass,
			"disabled_at":       keyDisabledAt,
		}
		if err := tx.Model(&model.ChannelKey{}).Where("id = ?", k.ID).Updates(updateMap).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("update key %d: %w", k.ID, err)
		}

		keySummaries = append(keySummaries, ChannelTestKeySummary{
			KeyID:          k.ID,
			KeyPreview:     previewKeyProbe(k.ChannelKey),
			Remark:         k.Remark,
			Enabled:        keyEnabled,
			AutoDisabled:   keyAutoDisabled,
			DisabledReason: keyDisabledReason,
			DisabledClass:  keyDisabledClass,
			SuccessCount:   successCount,
			FailCount:      failCount,
			Models:         sortedResults(modelResults),
		})
	}

	channelEnabled := channel.Enabled
	channelAutoDisabled := channel.AutoDisabled
	channelDisabledReason := channel.DisabledReason
	channelDisabledClass := channel.DisabledClass
	channelDisabledAt := channel.DisabledAt

	allKeysDown := len(keySummaries) > 0
	allDownClass := model.ChannelTestErrorClass("")
	allDownReason := ""
	for _, ks := range keySummaries {
		if !ks.AutoDisabled {
			allKeysDown = false
			break
		}
		if allDownClass == "" {
			allDownClass = ks.DisabledClass
			allDownReason = ks.DisabledReason
		}
	}

	if allKeysDown && allDownClass != "" {
		if channelEnabled || !channelAutoDisabled {
			log.Warnf("auto-disabling channel %s: every key auto-disabled with %s", channel.Name, allDownClass)
		}
		channelEnabled = false
		channelAutoDisabled = true
		channelDisabledClass = allDownClass
		channelDisabledReason = allDownReason
		if channelDisabledAt == 0 {
			channelDisabledAt = now
		}
	} else if channel.AutoDisabled && !allKeysDown {
		log.Infof("auto-re-enabling channel %s (at least one key healthy)", channel.Name)
		channelEnabled = true
		channelAutoDisabled = false
		channelDisabledReason = ""
		channelDisabledClass = ""
		channelDisabledAt = 0
	}

	channelUpdates := map[string]interface{}{
		"last_test_at":    now,
		"enabled":         channelEnabled,
		"auto_disabled":   channelAutoDisabled,
		"disabled_reason": channelDisabledReason,
		"disabled_class":  channelDisabledClass,
		"disabled_at":     channelDisabledAt,
	}
	if err := tx.Model(&model.Channel{}).Where("id = ?", channel.ID).Updates(channelUpdates).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("update channel %d: %w", channel.ID, err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("commit test run: %w", err)
	}

	sort.Slice(keySummaries, func(i, j int) bool { return keySummaries[i].KeyID < keySummaries[j].KeyID })
	summary.Keys = keySummaries
	summary.SuccessCount = channelTotalSuccess
	summary.FailCount = channelTotalFail
	return nil
}

// ChannelTestResultsList returns every persisted test result for a channel,
// grouped by key. Used by the UI when the user expands a channel without
// re-running the test.
func ChannelTestResultsList(ctx context.Context, channelID int) (*ChannelTestSummary, error) {
	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return nil, err
	}
	var results []model.ChannelTestResult
	if err := db.GetDB().WithContext(ctx).
		Where("channel_id = ?", channelID).
		Find(&results).Error; err != nil {
		return nil, err
	}

	resultsByKey := make(map[int][]model.ChannelTestResult, len(channel.Keys))
	for _, r := range results {
		resultsByKey[r.KeyID] = append(resultsByKey[r.KeyID], r)
	}

	summary := &ChannelTestSummary{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		TotalKeys:   len(channel.Keys),
		TotalModels: len(channelModels(channel)),
		TestedAt:    channel.LastTestAt,
	}
	if channel.AutoDisabled || (!channel.Enabled && channel.DisabledReason != "") {
		summary.Disabled = &ChannelDisabledTagDetail{
			AutoDisabled: channel.AutoDisabled,
			Reason:       channel.DisabledReason,
			Class:        channel.DisabledClass,
			DisabledAt:   channel.DisabledAt,
		}
	}

	for _, k := range channel.Keys {
		mr := resultsByKey[k.ID]
		summary.Keys = append(summary.Keys, ChannelTestKeySummary{
			KeyID:          k.ID,
			KeyPreview:     previewKeyProbe(k.ChannelKey),
			Remark:         k.Remark,
			Enabled:        k.Enabled,
			AutoDisabled:   k.AutoDisabled,
			DisabledReason: k.DisabledReason,
			DisabledClass:  k.DisabledClass,
			SuccessCount:   k.LastTestSuccess,
			FailCount:      k.LastTestFailed,
			Models:         sortedResults(mr),
		})
		summary.SuccessCount += k.LastTestSuccess
		summary.FailCount += k.LastTestFailed
	}
	summary.TotalProbes = summary.SuccessCount + summary.FailCount
	sort.Slice(summary.Keys, func(i, j int) bool { return summary.Keys[i].KeyID < summary.Keys[j].KeyID })
	return summary, nil
}

func channelModels(channel *model.Channel) []string {
	return xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
}

func intersectModels(a, b []string) []string {
	want := make(map[string]struct{}, len(b))
	for _, s := range b {
		want[strings.TrimSpace(s)] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, s := range a {
		if _, ok := want[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

func filterKeys(keys []model.ChannelKey, includeDisabled bool) []model.ChannelKey {
	out := make([]model.ChannelKey, 0, len(keys))
	for _, k := range keys {
		if includeDisabled || k.Enabled {
			out = append(out, k)
		}
	}
	return out
}

func previewKeyProbe(key string) string {
	if len(key) <= 10 {
		return key
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func sortedResults(results []model.ChannelTestResult) []model.ChannelTestResult {
	out := make([]model.ChannelTestResult, len(results))
	copy(out, results)
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

func probeTimeout() time.Duration {
	v, err := op.SettingGetInt(model.SettingKeyHealthCheckProbeTimeout)
	if err != nil || v <= 0 {
		return 20 * time.Second
	}
	return time.Duration(v) * time.Second
}

// humanReason produces a short, user-friendly disabled-reason label for the
// given error class. The full upstream message is preserved separately on
// the per-model results.
func humanReason(class model.ChannelTestErrorClass, msg string) string {
	switch class {
	case model.ChannelTestErrorAuth:
		return "invalid api key"
	case model.ChannelTestErrorPermission:
		return "permission denied"
	case model.ChannelTestErrorQuota:
		return "insufficient quota / billing"
	}
	return strings.TrimSpace(msg)
}
