package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
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
			router.NewRoute("/test", http.MethodPost).
				Handle(testChannel),
		).
		AddRoute(
			router.NewRoute("/test-all", http.MethodPost).
				Handle(testAllChannels),
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
		)
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

	summary, err := task.ChannelTestResultsList(c.Request.Context(), request.ID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	summary.Running = true

	models := append([]string(nil), request.Models...)
	go func(channelID int, modelFilter []string, includeDisabledKeys bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
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

	go func(channels []model.Channel, includeDisabledKeys bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		for _, ch := range channels {
			if _, err := task.ChannelTestRun(ctx, ch.ID, nil, includeDisabledKeys); err != nil {
				log.Warnf("channel test-all failed for channel %d: %v", ch.ID, err)
			}
		}
	}(targetChannels, request.IncludeDisabledKeys)

	resp.Success(c, map[string]any{
		"summaries": summaries,
		"skipped":   skipped,
		"running":   true,
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
