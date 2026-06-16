package task

import (
	"context"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const autoDisabledKeyRemarkPrefix = "auto-disabled:"

func ChannelKeyRecheckTask() {
	log.Debugf("channel key recheck task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("channel key recheck task finished, recheck time: %s", time.Since(startTime))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	channels, err := op.ChannelList(ctx)
	if err != nil {
		log.Warnf("failed to list channels: %v", err)
		return
	}

	now := time.Now()
	for _, ch := range channels {
		// Only auto-recheck keys on enabled channels, or channels that were auto-disabled by the system.
		if !ch.Enabled && !ch.AutoDisabled {
			continue
		}
		if ch.GetBaseUrl() == "" {
			continue
		}

		for _, k := range ch.Keys {
			if k.Enabled {
				continue
			}
			remark := strings.TrimSpace(k.Remark)
			if !strings.HasPrefix(remark, autoDisabledKeyRemarkPrefix) {
				continue
			}
			cat := parseAutoDisabledCategory(remark)
			// Only recheck temporary categories; do not recheck no_money / invalid_key.
			if cat != "bad_gateway" {
				continue
			}
			if k.ChannelKey == "" {
				continue
			}

			// Avoid immediately retrying a key that was just disabled.
			if k.LastUseTimeStamp > 0 {
				lastUse := time.Unix(k.LastUseTimeStamp, 0)
				if now.Sub(lastUse) < 10*time.Minute {
					continue
				}
			}

			test := ch
			test.Keys = []model.ChannelKey{{
				ID:         k.ID,
				ChannelID:  ch.ID,
				Enabled:    true,
				ChannelKey: k.ChannelKey,
			}}

			models, err := helper.FetchModels(ctx, test)
			if err != nil || len(models) == 0 {
				continue
			}

			prev := strings.TrimSpace(k.Remark)
			k.Enabled = true
			k.StatusCode = 0
			k.LastUseTimeStamp = time.Now().Unix()
			k.Remark = "auto-reenabled: time=" + time.Now().UTC().Format(time.RFC3339)
			if prev != "" {
				k.Remark += " | prev=" + prev
			}

			if err := op.ChannelKeyUpdate(k); err != nil {
				log.Warnf("failed to re-enable channel key %d for channel %s: %v", k.ID, ch.Name, err)
				continue
			}

			if ch.AutoDisabled && !ch.Enabled {
				if err := op.ChannelAutoEnable(ch.ID, ctx); err != nil {
					log.Warnf("failed to auto-enable channel %s after key recovery: %v", ch.Name, err)
				} else {
					log.Infof("auto-enabled channel %s after key recovery", ch.Name)
				}
			}

			log.Infof("re-enabled channel key %d for channel %s", k.ID, ch.Name)
		}
	}
}

func parseAutoDisabledCategory(remark string) string {
	// Format: auto-disabled: category=<cat> ...
	idx := strings.Index(remark, "category=")
	if idx < 0 {
		return ""
	}
	s := remark[idx+len("category="):]
	end := strings.IndexByte(s, ' ')
	if end < 0 {
		end = len(s)
	}
	cat := strings.TrimSpace(s[:end])
	return cat
}
