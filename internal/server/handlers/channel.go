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
			router.NewRoute("/test-results", http.MethodGet).
				Handle(getTestResults),
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

// channelTestResponse is the per-channel payload returned by /test and used as
// the value side of /test-all and /test-results (no channel_id).
type channelTestResponse struct {
	Summary model.ChannelTestSummary       `json:"summary"`
	Results []model.ChannelKeyModelStatus  `json:"results"`
}

func summarizeResults(results []model.ChannelKeyModelStatus) model.ChannelTestSummary {
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
	return summary
}

func runChannelProbe(ctx context.Context, channelID int, sem chan struct{}) (channelTestResponse, error) {
	ch, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return channelTestResponse{}, err
	}
	probeResults := helper.ProbeChannel(ctx, ch, sem)
	statuses := make([]model.ChannelKeyModelStatus, 0, len(probeResults))
	for _, pr := range probeResults {
		statuses = append(statuses, pr.ToStatus())
	}
	if len(statuses) > 0 {
		if err := op.TestResultsUpsert(ctx, channelID, statuses); err != nil {
			return channelTestResponse{}, err
		}
	}
	stored := op.TestResultsByChannel(channelID)
	return channelTestResponse{
		Summary: summarizeResults(stored),
		Results: stored,
	}, nil
}

func testChannel(c *gin.Context) {
	var request struct {
		ChannelID int `json:"channel_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	out, err := runChannelProbe(ctx, request.ChannelID, helper.NewProbeSem())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, out)
}

func testAllChannels(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	channels, err := op.ChannelList(ctx)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	sem := helper.NewProbeSem()
	results := make(map[string]channelTestResponse, len(channels))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range channels {
		ch := channels[i]
		if !ch.Enabled {
			continue
		}
		wg.Add(1)
		go func(channelID int) {
			defer wg.Done()
			out, err := runChannelProbe(ctx, channelID, sem)
			if err != nil {
				return
			}
			mu.Lock()
			results[strconv.Itoa(channelID)] = out
			mu.Unlock()
		}(ch.ID)
	}
	wg.Wait()

	resp.Success(c, gin.H{"results": results})
}

func getTestResults(c *gin.Context) {
	idStr := c.Query("channel_id")
	if idStr != "" {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
			return
		}
		stored := op.TestResultsByChannel(id)
		resp.Success(c, channelTestResponse{
			Summary: summarizeResults(stored),
			Results: stored,
		})
		return
	}

	all := op.TestResultsAll()
	out := make(map[string]channelTestResponse, len(all))
	for cid, rows := range all {
		out[strconv.Itoa(cid)] = channelTestResponse{
			Summary: summarizeResults(rows),
			Results: rows,
		}
	}
	resp.Success(c, gin.H{"results": out})
}
