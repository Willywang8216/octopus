package op

import (
	"context"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm/clause"
)

// In-memory store: channelID -> []ChannelKeyModelStatus.
var testResultsCache = struct {
	sync.RWMutex
	byChannel map[int][]model.ChannelKeyModelStatus
}{byChannel: make(map[int][]model.ChannelKeyModelStatus)}

// TestResultsRefreshCache loads everything from DB into memory.
func TestResultsRefreshCache(ctx context.Context) error {
	var rows []model.ChannelKeyModelStatus
	if err := db.GetDB().WithContext(ctx).Find(&rows).Error; err != nil {
		return err
	}
	grouped := make(map[int][]model.ChannelKeyModelStatus, 32)
	for _, r := range rows {
		grouped[r.ChannelID] = append(grouped[r.ChannelID], r)
	}
	testResultsCache.Lock()
	testResultsCache.byChannel = grouped
	testResultsCache.Unlock()
	return nil
}

// TestResultsByChannel returns a snapshot copy of cached results for one
// channel. Returns nil if the channel has never been probed.
func TestResultsByChannel(channelID int) []model.ChannelKeyModelStatus {
	testResultsCache.RLock()
	defer testResultsCache.RUnlock()
	src := testResultsCache.byChannel[channelID]
	if len(src) == 0 {
		return nil
	}
	out := make([]model.ChannelKeyModelStatus, len(src))
	copy(out, src)
	return out
}

// TestResultsAll returns a snapshot copy of all cached results grouped by
// channel id.
func TestResultsAll() map[int][]model.ChannelKeyModelStatus {
	testResultsCache.RLock()
	defer testResultsCache.RUnlock()
	out := make(map[int][]model.ChannelKeyModelStatus, len(testResultsCache.byChannel))
	for k, v := range testResultsCache.byChannel {
		cp := make([]model.ChannelKeyModelStatus, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// TestResultsUpsert upserts the given probe results for a channel and
// refreshes the cache from DB. Records for combos NOT in the new batch are
// LEFT UNTOUCHED so partial probes do not wipe out unrelated rows.
func TestResultsUpsert(ctx context.Context, channelID int, results []model.ChannelKeyModelStatus) error {
	if len(results) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for i := range results {
		results[i].ChannelID = channelID
		if results[i].LastTestedAt == 0 {
			results[i].LastTestedAt = now
		}
	}

	tx := db.GetDB().WithContext(ctx)
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "channel_id"},
			{Name: "key_id"},
			{Name: "model_name"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"ok", "status_code", "latency_ms", "last_error", "error_class", "last_tested_at",
		}),
	}).Create(&results).Error; err != nil {
		return err
	}

	// Reload all rows for this channel so we see freshly assigned IDs.
	var fresh []model.ChannelKeyModelStatus
	if err := tx.Where("channel_id = ?", channelID).Find(&fresh).Error; err != nil {
		return err
	}
	testResultsCache.Lock()
	testResultsCache.byChannel[channelID] = fresh
	testResultsCache.Unlock()
	return nil
}

// TestResultsDelByChannel removes a channel's entries (call when channel is
// deleted).
func TestResultsDelByChannel(ctx context.Context, channelID int) error {
	if err := db.GetDB().WithContext(ctx).
		Where("channel_id = ?", channelID).
		Delete(&model.ChannelKeyModelStatus{}).Error; err != nil {
		return err
	}
	testResultsCache.Lock()
	delete(testResultsCache.byChannel, channelID)
	testResultsCache.Unlock()
	return nil
}

// TestResultsDelByKey removes entries for a given key (call when a key is
// removed from a channel).
func TestResultsDelByKey(ctx context.Context, channelID, keyID int) error {
	if err := db.GetDB().WithContext(ctx).
		Where("channel_id = ? AND key_id = ?", channelID, keyID).
		Delete(&model.ChannelKeyModelStatus{}).Error; err != nil {
		return err
	}
	testResultsCache.Lock()
	src := testResultsCache.byChannel[channelID]
	if len(src) > 0 {
		next := make([]model.ChannelKeyModelStatus, 0, len(src))
		for _, r := range src {
			if r.KeyID != keyID {
				next = append(next, r)
			}
		}
		testResultsCache.byChannel[channelID] = next
	}
	testResultsCache.Unlock()
	return nil
}
