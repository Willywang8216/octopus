package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/task"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listChannel),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Handle(createChannel),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Handle(updateChannel),
		).
		AddRoute(
			router.NewRoute("/enable", http.MethodPost).
				Handle(enableChannel),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Handle(deleteChannel),
		).
		AddRoute(
			router.NewRoute("/fetch-model", http.MethodPost).
				Handle(fetchModel),
		).
		AddRoute(
			router.NewRoute("/check-duplicate", http.MethodPost).
				Handle(checkDuplicateChannel),
		).
		AddRoute(
			router.NewRoute("/combine", http.MethodPost).
				Handle(combineChannel),
		).
		AddRoute(
			router.NewRoute("/test", http.MethodPost).
				Handle(testChannel),
		).
		AddRoute(
			router.NewRoute("/test-all", http.MethodPost).
				Handle(testAllChannels),
		).
		AddRoute(
			router.NewRoute("/cancel-test", http.MethodPost).
				Handle(cancelChannelTest),
		)
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/sync", http.MethodPost).
				Handle(syncChannel),
		).
		AddRoute(
			router.NewRoute("/last-sync-time", http.MethodGet).
				Handle(getLastSyncTime),
		).
		AddRoute(
			router.NewRoute("/test-results/:id", http.MethodGet).
				Handle(getChannelTestResults),
		).
		AddRoute(
			router.NewRoute("/test-all-status", http.MethodGet).
				Handle(getChannelTestAllStatus),
		).
		AddRoute(
			router.NewRoute("/test-progress/:id", http.MethodGet).
				Handle(getChannelTestProgress),
		)
}

type channelTestAllStatus struct {
	Running           bool   `json:"running"`
	Cancelled         bool   `json:"cancelled"`
	StartedAt         int64  `json:"started_at"`
	FinishedAt        int64  `json:"finished_at"`
	TotalChannels     int    `json:"total_channels"`
	CompletedChannels int    `json:"completed_channels"`
	FailedChannels    int    `json:"failed_channels"`
	LastError         string `json:"last_error"`
}

var channelTestAllState = struct {
	sync.Mutex
	status channelTestAllStatus
	cancel context.CancelFunc
}{}

var channelTestCancels sync.Map // channelID -> context.CancelFunc

func snapshotChannelTestAllStatus() channelTestAllStatus {
	channelTestAllState.Lock()
	defer channelTestAllState.Unlock()
	return channelTestAllState.status
}

func startChannelTestAllStatus(total int) (channelTestAllStatus, bool) {
	channelTestAllState.Lock()
	defer channelTestAllState.Unlock()
	if channelTestAllState.status.Running {
		return channelTestAllState.status, false
	}
	now := time.Now().Unix()
	channelTestAllState.status = channelTestAllStatus{
		Running:       total > 0,
		Cancelled:     false,
		StartedAt:     now,
		FinishedAt:    now,
		TotalChannels: total,
	}
	channelTestAllState.cancel = nil
	if total > 0 {
		channelTestAllState.status.FinishedAt = 0
	}
	return channelTestAllState.status, total > 0
}

func updateChannelTestAllStatus(err error) {
	channelTestAllState.Lock()
	defer channelTestAllState.Unlock()
	channelTestAllState.status.CompletedChannels++
	if err != nil {
		channelTestAllState.status.FailedChannels++
		channelTestAllState.status.LastError = err.Error()
	}
	if channelTestAllState.status.CompletedChannels >= channelTestAllState.status.TotalChannels {
		channelTestAllState.status.Running = false
		channelTestAllState.status.FinishedAt = time.Now().Unix()
	}
}

func listChannel(c *gin.Context) {
	channels, err := op.ChannelList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	for i, channel := range channels {
		stats := op.StatsChannelGet(channel.ID)
		channels[i].Stats = &stats

		results := op.TestResultsByChannel(channel.ID)
		summary := model.ChannelTestSummary{Total: len(results)}
		for _, r := range results {
			if r.OK {
				summary.Ok++
			} else {
				summary.Failed++
			}
			if r.LastTestedAt > summary.LastTestedAt {
				summary.LastTestedAt = r.LastTestedAt
			}
		}
		summary.Health = helper.DeriveHealth(results)
		channels[i].Health = summary.Health
		channels[i].TestSummary = &summary
		channels[i].TestProgress = task.ChannelTestProgressGet(channel.ID)
	}
	resp.Success(c, channels)
}

func createChannel(c *gin.Context) {
	var channel model.Channel
	if err := c.ShouldBindJSON(&channel); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	// Check for duplicate API endpoint + key combinations.
	keyStrs := make([]string, 0, len(channel.Keys))
	for _, k := range channel.Keys {
		keyStrs = append(keyStrs, k.ChannelKey)
	}
	if err := op.ChannelCheckDuplicate(channel.BaseUrls, keyStrs, 0); err != nil {
		resp.Error(c, http.StatusConflict, err.Error())
		return
	}
	if err := op.ChannelCreate(&channel, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(&channel)
	resp.Success(c, channel)
}

func updateChannel(c *gin.Context) {
	var req model.ChannelUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	// Check for duplicate API endpoint + key combinations when adding new keys.
	if len(req.KeysToAdd) > 0 {
		// Use the channel's current base URLs or the updated ones.
		existing, _ := op.ChannelGet(req.ID, c.Request.Context())
		var baseUrls []model.BaseUrl
		if req.BaseUrls != nil {
			baseUrls = *req.BaseUrls
		} else if existing != nil {
			baseUrls = existing.BaseUrls
		}
		newKeyStrs := make([]string, 0, len(req.KeysToAdd))
		for _, k := range req.KeysToAdd {
			newKeyStrs = append(newKeyStrs, k.ChannelKey)
		}
		if err := op.ChannelCheckDuplicate(baseUrls, newKeyStrs, req.ID); err != nil {
			resp.Error(c, http.StatusConflict, err.Error())
			return
		}
	}
	channel, err := op.ChannelUpdate(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(channel)
	resp.Success(c, channel)
}

func enableChannel(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := op.ChannelEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func deleteChannel(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if err := op.ChannelDel(idNum, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}
func fetchModel(c *gin.Context) {
	var request model.Channel
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	models, err := helper.FetchAvailableModels(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, models)
}

func checkDuplicateChannel(c *gin.Context) {
	var request struct {
		BaseUrls  []model.BaseUrl `json:"base_urls"`
		Keys      []string        `json:"keys"`
		ExcludeID int             `json:"exclude_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	resp.Success(c, op.ChannelFindDuplicates(request.BaseUrls, request.Keys, request.ExcludeID))
}

func combineChannel(c *gin.Context) {
	var request struct {
		TargetID     int                          `json:"target_id"`
		BaseUrls     []model.BaseUrl              `json:"base_urls"`
		Keys         []model.ChannelKeyAddRequest `json:"keys"`
		Model        string                       `json:"model"`
		CustomModel  string                       `json:"custom_model"`
		CustomHeader []model.CustomHeader         `json:"custom_header"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if request.TargetID <= 0 {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	channel, err := op.ChannelCombineInto(request.TargetID, request.BaseUrls, request.Keys, request.Model, request.CustomModel, request.CustomHeader, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	resp.Success(c, channel)
}

func syncChannel(c *gin.Context) {
	task.SyncModelsTask()
	resp.Success(c, nil)
}

func getLastSyncTime(c *gin.Context) {
	time := task.GetLastSyncModelsTime()
	resp.Success(c, time)
}

// testChannel starts the probe matrix for a single channel in the background
// and returns immediately with the latest cached result. Probing every
// key×model combination can exceed browser/reverse-proxy timeouts, so the
// request path must stay short and let the UI refresh cached results later.
func testChannel(c *gin.Context) {
	var request struct {
		ID     int      `json:"id"`
		Models []string `json:"models,omitempty"`
		// IncludeDisabledKeys allows the UI to (re)test keys the user has
		// manually disabled or that the auto-disable logic has switched off.
		IncludeDisabledKeys bool `json:"include_disabled_keys,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if request.ID <= 0 {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}

	channel, err := op.ChannelGet(request.ID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if channel.SkipTest {
		resp.Error(c, http.StatusBadRequest, "channel is marked to skip testing")
		return
	}
	modelText := strings.Trim(strings.TrimSpace(channel.Model+","+channel.CustomModel), ",")
	totalModels := 0
	if modelText != "" {
		totalModels = len(strings.Split(modelText, ","))
	}
	summary := &task.ChannelTestSummary{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		TotalKeys:   len(channel.Keys),
		TotalModels: totalModels,
		TestedAt:    channel.LastTestAt,
		Running:     true,
	}

	models := append([]string(nil), request.Models...)
	go func(channelID int, modelFilter []string, includeDisabledKeys bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		channelTestCancels.Store(channelID, cancel)
		defer channelTestCancels.Delete(channelID)
		if _, err := task.ChannelTestRun(ctx, channelID, modelFilter, includeDisabledKeys); err != nil {
			log.Warnf("channel test failed for channel %d: %v", channelID, err)
		}
	}(request.ID, models, request.IncludeDisabledKeys)

	resp.Success(c, summary)
}

// testAllChannels starts a background probe across every selected channel and
// returns immediately with cached summaries. This avoids 504s from long
// provider checks while still letting the health matrix update as results land.
func testAllChannels(c *gin.Context) {
	var request struct {
		IncludeDisabledKeys bool `json:"include_disabled_keys,omitempty"`
		// IncludeDisabledChannels allows the user to also probe channels
		// that have been disabled (manually or automatically). Without this
		// only enabled channels are scanned.
		IncludeDisabledChannels bool `json:"include_disabled_channels,omitempty"`
	}
	// JSON body is optional for this endpoint; ignore parse errors so an
	// empty body still works.
	_ = c.ShouldBindJSON(&request)

	channels, err := op.ChannelList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	targetChannels := make([]model.Channel, 0, len(channels))
	summaries := make([]*task.ChannelTestSummary, 0, len(channels))
	skipped := make([]map[string]any, 0)
	for _, ch := range channels {
		if ch.SkipTest {
			skipped = append(skipped, map[string]any{
				"channel_id":   ch.ID,
				"channel_name": ch.Name,
				"reason":       "skip_test enabled",
			})
			continue
		}
		if !request.IncludeDisabledChannels && !ch.Enabled {
			skipped = append(skipped, map[string]any{
				"channel_id":   ch.ID,
				"channel_name": ch.Name,
				"reason":       "channel disabled",
			})
			continue
		}
		targetChannels = append(targetChannels, ch)
		summary, err := task.ChannelTestResultsList(c.Request.Context(), ch.ID)
		if err != nil {
			skipped = append(skipped, map[string]any{
				"channel_id":   ch.ID,
				"channel_name": ch.Name,
				"reason":       err.Error(),
			})
			continue
		}
		summaries = append(summaries, summary)
	}

	status, started := startChannelTestAllStatus(len(targetChannels))
	if started {
		go func(channels []model.Channel, includeDisabledKeys bool) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			channelTestAllState.Lock()
			channelTestAllState.cancel = cancel
			channelTestAllState.Unlock()
			for _, ch := range channels {
				_, err := task.ChannelTestRun(ctx, ch.ID, nil, includeDisabledKeys)
				if err != nil {
					log.Warnf("channel test-all failed for channel %d: %v", ch.ID, err)
				}
				updateChannelTestAllStatus(err)
			}
			if ctx.Err() == context.Canceled {
				channelTestAllState.Lock()
				channelTestAllState.status.Cancelled = true
				channelTestAllState.status.Running = false
				channelTestAllState.status.FinishedAt = time.Now().Unix()
				channelTestAllState.cancel = nil
				channelTestAllState.Unlock()
			}
		}(targetChannels, request.IncludeDisabledKeys)
	}

	resp.Success(c, map[string]any{
		"summaries": summaries,
		"skipped":   skipped,
		"running":   status.Running,
		"status":    status,
	})
}

// getChannelTestResults returns the most recently persisted test results for
// a channel without re-running the probes. The UI uses this to populate the
// channel detail panel on first open.
func getChannelTestResults(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	summary, err := task.ChannelTestResultsList(c.Request.Context(), idNum)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, summary)
}

func getChannelTestAllStatus(c *gin.Context) {
	resp.Success(c, snapshotChannelTestAllStatus())
}

func cancelChannelTest(c *gin.Context) {
	var request struct {
		ChannelID int `json:"channel_id,omitempty"`
	}
	_ = c.ShouldBindJSON(&request)

	if request.ChannelID > 0 {
		if cancel, ok := channelTestCancels.Load(request.ChannelID); ok {
			cancel.(context.CancelFunc)()
			resp.Success(c, map[string]any{"cancelled": true, "channel_id": request.ChannelID})
			return
		}
		resp.Success(c, map[string]any{"cancelled": false, "channel_id": request.ChannelID})
		return
	}

	channelTestAllState.Lock()
	cancel := channelTestAllState.cancel
	channelTestAllState.Unlock()
	if cancel != nil {
		cancel()
		resp.Success(c, map[string]any{"cancelled": true})
		return
	}
	resp.Success(c, map[string]any{"cancelled": false})
}

func getChannelTestProgress(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	progress := task.ChannelTestProgressGet(idNum)
	if progress == nil {
		resp.Success(c, nil)
		return
	}
	resp.Success(c, progress)
}
