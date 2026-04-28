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
			router.NewRoute("/test/start", http.MethodPost).
				Handle(startChannelTest),
		).
		AddRoute(
			router.NewRoute("/test/cancel", http.MethodPost).
				Handle(cancelChannelTest),
		).
		AddRoute(
			router.NewRoute("/test/status", http.MethodGet).
				Handle(getChannelTestStatus),
		).
		AddRoute(
			router.NewRoute("/test/results", http.MethodGet).
				Handle(getChannelTestResults),
		).
		AddRoute(
			router.NewRoute("/test/results/:id", http.MethodGet).
				Handle(getChannelTestResult),
		)
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/check-models", http.MethodPost).
				Handle(checkModels),
		).
		AddRoute(
			router.NewRoute("/test-model", http.MethodPost).
				Handle(testModel),
		).
		AddRoute(
			router.NewRoute("/check-duplicate", http.MethodPost).
				Handle(checkDuplicate),
		).
		AddRoute(
			router.NewRoute("/test-all-models", http.MethodPost).
				Handle(testAllModels),
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

func syncChannel(c *gin.Context) {
	task.SyncModelsTask()
	resp.Success(c, nil)
}

func getLastSyncTime(c *gin.Context) {
	time := task.GetLastSyncModelsTime()
	resp.Success(c, time)
}

// startChannelTestRequest is the optional payload for /channel/test/start.
// When `channel_ids` is empty or absent, every enabled channel is tested.
type startChannelTestRequest struct {
	ChannelIDs []int `json:"channel_ids"`
}

func startChannelTest(c *gin.Context) {
	var req startChannelTestRequest
	// Body is optional; ignore parse errors when there is no body to read.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
			return
		}
	}
	if err := task.StartChannelTest(req.ChannelIDs); err != nil {
		// 409 Conflict communicates "already running" without scaring the
		// frontend into showing an error toast for what is essentially a
		// benign duplicate-click.
		resp.Error(c, http.StatusConflict, err.Error())
		return
	}
	resp.Success(c, task.ChannelTestStatus())
}

func cancelChannelTest(c *gin.Context) {
	task.CancelChannelTest()
	resp.Success(c, task.ChannelTestStatus())
}

func getChannelTestStatus(c *gin.Context) {
	resp.Success(c, task.ChannelTestStatus())
}

func getChannelTestResults(c *gin.Context) {
	resp.Success(c, task.ChannelTestAllResults())
}

func getChannelTestResult(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	result := task.ChannelTestResult(id)
	if result == nil {
		resp.Error(c, http.StatusNotFound, "no test result for this channel")
		return
	}
	resp.Success(c, result)
}
