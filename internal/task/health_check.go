package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// ChannelHealthCheckTask re-probes any auto-disabled keys / channels so a
// transient failure (e.g. rate limit, expired free-tier window, briefly
// missing billing) can recover automatically. Manually-disabled keys and
// channels are NOT touched — they were turned off by the user and only the
// user should turn them back on.
func ChannelHealthCheckTask() {
	startTime := time.Now()
	defer func() {
		log.Debugf("channel health check task finished in %s", time.Since(startTime))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	channels, err := op.ChannelList(ctx)
	if err != nil {
		log.Warnf("channel health check: list channels failed: %v", err)
		return
	}

	for _, ch := range channels {
		// Identify channels that have something to probe. We only care about
		// auto-disabled artefacts; leave manually-managed state alone.
		channelAutoDisabled := ch.AutoDisabled
		hasAutoDisabledKey := false
		for _, k := range ch.Keys {
			if k.AutoDisabled {
				hasAutoDisabledKey = true
				break
			}
		}
		if !channelAutoDisabled && !hasAutoDisabledKey {
			continue
		}

		// forceAllKeys=true so the prober re-evaluates auto-disabled keys
		// even though their `Enabled` flag is currently false. The probe
		// will flip them back on if they pass.
		if _, err := ChannelTestRun(ctx, ch.ID, nil, true); err != nil {
			log.Debugf("channel %s (id=%d) health check skipped: %v", ch.Name, ch.ID, err)
			continue
		}
	}
}
