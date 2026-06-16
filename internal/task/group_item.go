package task

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const autoDisabledGroupItemRemarkPrefix = "auto-disabled:"

func GroupItemRecheckTask() {
	log.Debugf("group item recheck task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("group item recheck task finished, recheck time: %s", time.Since(startTime))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	groups, err := op.GroupList(ctx)
	if err != nil {
		log.Warnf("failed to list groups: %v", err)
		return
	}

	now := time.Now()
	for _, g := range groups {
		for _, item := range g.Items {
			if item.Enabled {
				continue
			}
			if !strings.HasPrefix(strings.TrimSpace(item.DisabledReason), autoDisabledGroupItemRemarkPrefix) {
				continue
			}
			if item.DisabledAt > 0 {
				disabledAt := time.Unix(item.DisabledAt, 0)
				if now.Sub(disabledAt) < 30*time.Minute {
					continue
				}
			}

			ch, err := op.ChannelGet(item.ChannelID, ctx)
			if err != nil {
				continue
			}
			if !ch.Enabled {
				continue
			}
			if ch.GetBaseUrl() == "" {
				continue
			}

			key := ch.GetChannelKey()
			if key.ChannelKey == "" {
				continue
			}

			test := *ch
			test.Keys = []model.ChannelKey{{
				ID:         key.ID,
				ChannelID:  ch.ID,
				Enabled:    true,
				ChannelKey: key.ChannelKey,
			}}

			models, err := helper.FetchModels(ctx, test)
			if err != nil || len(models) == 0 {
				continue
			}
			if !slices.Contains(models, item.ModelName) {
				continue
			}

			if err := op.GroupItemEnable(item.ID, ctx); err != nil {
				log.Warnf("failed to re-enable group item %d: %v", item.ID, err)
				continue
			}
			log.Infof("re-enabled group item %d (group=%s channel=%d model=%s)", item.ID, g.Name, item.ChannelID, item.ModelName)
		}
	}
}
