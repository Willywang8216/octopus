package task

import (
	"context"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/diff"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

func ChannelBaseUrlDelayTask() {
	log.Debugf("channel base url delay task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("channel base url delay task finished, update time: %s", time.Since(startTime))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	channels, err := op.ChannelList(ctx)
	if err != nil {
		log.Errorf("failed to list channels: %v", err)
		return
	}
	for _, channel := range channels {
		helper.ChannelBaseUrlDelayUpdate(&channel, ctx)
	}
}

// AutoDisableRetryTask re-enables channels that were auto-disabled after the
// configured retry period has elapsed.
func AutoDisableRetryTask() {
	log.Debugf("auto-disable retry task started")
	retryHours, err := op.SettingGetInt(model.SettingKeyAutoDisableRetryHours)
	if err != nil || retryHours <= 0 {
		retryHours = 24
	}
	retryDuration := time.Duration(retryHours) * time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	channels, err := op.ChannelList(ctx)
	if err != nil {
		log.Errorf("auto-disable retry: failed to list channels: %v", err)
		return
	}

	now := time.Now().Unix()
	for _, ch := range channels {
		if ch.StatusTag != model.StatusTagAutoDisabled || ch.AutoDisabledAt == nil {
			continue
		}
		elapsed := time.Duration(now-*ch.AutoDisabledAt) * time.Second
		if elapsed >= retryDuration {
			log.Infof("auto-disable retry: re-enabling channel %s (id=%d) after %v", ch.Name, ch.ID, elapsed)
			if err := op.ChannelClearAutoDisabled(ch.ID, ctx); err != nil {
				log.Errorf("auto-disable retry: failed to re-enable channel %d: %v", ch.ID, err)
			}
		}
	}
}

var lastModelCheckTime = time.Now()

// ModelAvailabilityCheckTask checks whether the model list for each auto-sync
// channel is still up-to-date by fetching the live model list from the provider.
func ModelAvailabilityCheckTask() {
	log.Debugf("model availability check task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("model availability check task finished in %s", time.Since(startTime))
		lastModelCheckTime = time.Now()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	channels, err := op.ChannelList(ctx)
	if err != nil {
		log.Errorf("model check: failed to list channels: %v", err)
		return
	}

	for _, channel := range channels {
		if !channel.AutoSync || !channel.Enabled {
			continue
		}

		fetchModels, err := helper.FetchModels(ctx, channel)
		if err != nil {
			log.Warnf("model check: failed to fetch models for channel %s: %v", channel.Name, err)
			continue
		}

		oldModels := xstrings.SplitTrimCompact(",", channel.Model)
		newModels := xstrings.TrimCompact(fetchModels)

		deletedModels, addedModels := diff.Diff(oldModels, newModels)
		if len(deletedModels) == 0 && len(addedModels) == 0 {
			continue
		}

		log.Infof("model check: channel %s — removed %d models, added %d models",
			channel.Name, len(deletedModels), len(addedModels))

		fetchModelStr := strings.Join(newModels, ",")
		if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{
			ID:    channel.ID,
			Model: &fetchModelStr,
		}, ctx); err != nil {
			log.Errorf("model check: failed to update channel %s: %v", channel.Name, err)
			continue
		}

		// Remove dead model group items.
		if len(deletedModels) > 0 {
			keys := make([]model.GroupIDAndLLMName, len(deletedModels))
			for i, m := range deletedModels {
				keys[i] = model.GroupIDAndLLMName{ChannelID: channel.ID, ModelName: m}
			}
			if err := op.GroupItemBatchDelByChannelAndModels(keys, ctx); err != nil {
				log.Errorf("model check: failed to remove dead group items for channel %s: %v", channel.Name, err)
			}
		}

		// Auto-group newly discovered models.
		if len(addedModels) > 0 {
			helper.ChannelAutoGroup(&channel, ctx)
		}
	}
}

// CheckModelsForChannel checks model availability for a single channel.
func CheckModelsForChannel(channelID int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return err
	}

	fetchModels, err := helper.FetchModels(ctx, *channel)
	if err != nil {
		return err
	}

	oldModels := xstrings.SplitTrimCompact(",", channel.Model)
	newModels := xstrings.TrimCompact(fetchModels)

	deletedModels, addedModels := diff.Diff(oldModels, newModels)
	if len(deletedModels) == 0 && len(addedModels) == 0 {
		return nil
	}

	fetchModelStr := strings.Join(newModels, ",")
	if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{
		ID:    channel.ID,
		Model: &fetchModelStr,
	}, ctx); err != nil {
		return err
	}

	if len(deletedModels) > 0 {
		keys := make([]model.GroupIDAndLLMName, len(deletedModels))
		for i, m := range deletedModels {
			keys[i] = model.GroupIDAndLLMName{ChannelID: channel.ID, ModelName: m}
		}
		_ = op.GroupItemBatchDelByChannelAndModels(keys, ctx)
	}

	if len(addedModels) > 0 {
		helper.ChannelAutoGroup(channel, ctx)
	}
	return nil
}

// GetLastModelCheckTime returns when models were last checked.
func GetLastModelCheckTime() time.Time {
	return lastModelCheckTime
}
