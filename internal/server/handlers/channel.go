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
	models, err := helper.FetchModels(c.Request.Context(), request)
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

func checkModels(c *gin.Context) {
	var request struct {
		ID int `json:"id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	if request.ID > 0 {
		// Check a specific channel.
		go func() {
			if err := task.CheckModelsForChannel(request.ID); err != nil {
				log.Warnf("check-models for channel %d failed: %v", request.ID, err)
			}
		}()
	} else {
		// Check all channels.
		go task.ModelAvailabilityCheckTask()
	}
	resp.Success(c, nil)
}

func testModel(c *gin.Context) {
	var request struct {
		ChannelID int    `json:"channel_id" binding:"required"`
		Model     string `json:"model" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	channel, err := op.ChannelGet(request.ChannelID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "channel not found")
		return
	}

	ok, err := helper.TestModelAvailability(c.Request.Context(), *channel, request.Model)
	if err != nil {
		resp.Success(c, map[string]interface{}{"available": false, "error": err.Error()})
		return
	}
	resp.Success(c, map[string]interface{}{"available": ok})
}

func checkDuplicate(c *gin.Context) {
	var request struct {
		BaseUrls  []model.BaseUrl `json:"base_urls"`
		Keys      []string        `json:"keys"`
		ExcludeID int             `json:"exclude_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	dupes := op.ChannelFindDuplicates(request.BaseUrls, request.Keys, request.ExcludeID)
	resp.Success(c, dupes)
}

func testAllModels(c *gin.Context) {
	var request struct {
		ChannelID int `json:"channel_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	channel, err := op.ChannelGet(request.ChannelID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "channel not found")
		return
	}

	// Collect all models from both auto and custom.
	allModels := make([]string, 0)
	if channel.Model != "" {
		for _, m := range strings.Split(channel.Model, ",") {
			m = strings.TrimSpace(m)
			if m != "" {
				allModels = append(allModels, m)
			}
		}
	}
	if channel.CustomModel != "" {
		for _, m := range strings.Split(channel.CustomModel, ",") {
			m = strings.TrimSpace(m)
			if m != "" {
				allModels = append(allModels, m)
			}
		}
	}

	type modelResult struct {
		Model     string `json:"model"`
		Available bool   `json:"available"`
		Error     string `json:"error,omitempty"`
	}

	results := make([]modelResult, 0, len(allModels))
	for _, m := range allModels {
		ok, testErr := helper.TestModelAvailability(c.Request.Context(), *channel, m)
		r := modelResult{Model: m, Available: ok}
		if testErr != nil {
			r.Error = testErr.Error()
		}
		results = append(results, r)
	}
	resp.Success(c, results)
}
