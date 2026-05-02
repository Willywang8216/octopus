package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// ChannelHealthCheckTask periodically refreshes channel probe results so the UI
// does not sit at "no result" forever. Enabled channels are tested normally;
// auto-disabled channels/keys are also tested with disabled keys included so
// recoverable failures can flip back to healthy. Manually-disabled channels are
// not touched unless they were auto-disabled by the system.
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
		channelAutoDisabled := ch.AutoDisabled
		hasAutoDisabledKey := false
		for _, k := range ch.Keys {
			if k.AutoDisabled {
				hasAutoDisabledKey = true
				break
			}
		}

		if !ch.Enabled && !channelAutoDisabled && !hasAutoDisabledKey {
			continue
		}

		includeDisabledKeys := channelAutoDisabled || hasAutoDisabledKey
		if _, err := ChannelTestRun(ctx, ch.ID, nil, includeDisabledKeys); err != nil {
			log.Debugf("channel %s (id=%d) health check skipped: %v", ch.Name, ch.ID, err)
			continue
		}
	}
}
