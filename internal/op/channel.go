package op

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

var channelCache = cache.New[int, model.Channel](16)
var channelKeyCache = cache.New[int, model.ChannelKey](16)
var channelKeyCacheNeedUpdate = make(map[int]struct{})
var channelKeyCacheNeedUpdateLock sync.Mutex
var channelCacheNeedUpdate = make(map[int]struct{})
var channelCacheNeedUpdateLock sync.Mutex

func ChannelList(ctx context.Context) ([]model.Channel, error) {
	channels := make([]model.Channel, 0, channelCache.Len())
	for _, channel := range channelCache.GetAll() {
		channels = append(channels, channel)
	}
	return channels, nil
}

func ChannelCreate(channel *model.Channel, ctx context.Context) error {
	if err := validateChannelDuplicates(0, channel.BaseUrls, channel.Keys); err != nil {
		return err
	}
	if err := db.GetDB().WithContext(ctx).Create(channel).Error; err != nil {
		return err
	}
	channelCache.Set(channel.ID, *channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	return nil
}

func normalizeBaseURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return strings.TrimRight(strings.ToLower(rawURL), "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/")
}

func normalizeChannelKey(key string) string {
	return strings.TrimSpace(key)
}

func validateChannelDuplicates(channelID int, baseUrls []model.BaseUrl, keys []model.ChannelKey) error {
	baseURLSet := make(map[string]struct{}, len(baseUrls))
	for _, baseURL := range baseUrls {
		normalizedURL := normalizeBaseURL(baseURL.URL)
		if normalizedURL == "" {
			continue
		}
		if _, ok := baseURLSet[normalizedURL]; ok {
			return fmt.Errorf("duplicate base url: %s", baseURL.URL)
		}
		baseURLSet[normalizedURL] = struct{}{}
	}

	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		normalizedKey := normalizeChannelKey(key.ChannelKey)
		if normalizedKey == "" {
			continue
		}
		if _, ok := keySet[normalizedKey]; ok {
			return fmt.Errorf("duplicate api key")
		}
		keySet[normalizedKey] = struct{}{}
	}

	for _, channel := range channelCache.GetAll() {
		if channel.ID == channelID {
			continue
		}
		for _, baseURL := range channel.BaseUrls {
			normalizedURL := normalizeBaseURL(baseURL.URL)
			if normalizedURL == "" {
				continue
			}
			if _, ok := baseURLSet[normalizedURL]; ok {
				return fmt.Errorf("base url already exists in channel %s", channel.Name)
			}
		}
		for _, key := range channel.Keys {
			normalizedKey := normalizeChannelKey(key.ChannelKey)
			if normalizedKey == "" {
				continue
			}
			if _, ok := keySet[normalizedKey]; ok {
				return fmt.Errorf("api key already exists in channel %s", channel.Name)
			}
		}
	}
	return nil
}

// ChannelKeyUpdate 仅更新 ChannelKey 的内存缓存（不落库），并标记为需要在 SaveCache 时写入数据库。
func ChannelKeyUpdate(key model.ChannelKey) error {
	if key.ID == 0 || key.ChannelID == 0 {
		return fmt.Errorf("invalid channel key")
	}
	ch, ok := channelCache.Get(key.ChannelID)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if len(ch.Keys) > 0 {
		keys := make([]model.ChannelKey, len(ch.Keys))
		copy(keys, ch.Keys)
		for i := range keys {
			if keys[i].ID == key.ID {
				keys[i] = key
				break
			}
		}
		ch.Keys = keys
	}
	channelCache.Set(key.ChannelID, ch)
	channelKeyCache.Set(key.ID, key)
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate[key.ID] = struct{}{}
	channelKeyCacheNeedUpdateLock.Unlock()
	return nil
}

func ChannelMarkKeySuccess(key model.ChannelKey) error {
	key.FailureCount = 0
	key.RetryAfter = 0
	key.LastError = ""
	return ChannelKeyUpdate(key)
}

func ChannelMarkKeyFailure(key model.ChannelKey, statusCode int, err error, billingIssue bool) error {
	key.StatusCode = statusCode
	key.LastUseTimeStamp = time.Now().Unix()
	key.FailureCount++
	if err != nil {
		key.LastError = err.Error()
	}

	threshold, _ := SettingGetInt(model.SettingKeyCircuitBreakerThreshold)
	if threshold <= 0 {
		threshold = 5
	}
	baseCooldown, _ := SettingGetInt(model.SettingKeyCircuitBreakerCooldown)
	if baseCooldown <= 0 {
		baseCooldown = 60
	}
	maxCooldown, _ := SettingGetInt(model.SettingKeyCircuitBreakerMaxCooldown)
	if maxCooldown <= 0 {
		maxCooldown = 600
	}
	if billingIssue || key.FailureCount >= threshold {
		shift := key.FailureCount - threshold
		if shift < 0 {
			shift = 0
		}
		if shift > 20 {
			shift = 20
		}
		cooldown := baseCooldown << shift
		if cooldown > maxCooldown {
			cooldown = maxCooldown
		}
		key.RetryAfter = time.Now().Add(time.Duration(cooldown) * time.Second).Unix()
	}

	return ChannelKeyUpdate(key)
}

func ChannelSetTags(id int, tags []model.ChannelTag, retryAfter int64, enabled *bool, ctx context.Context) error {
	ch, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	updates := map[string]interface{}{
		"tags":        tags,
		"retry_after": retryAfter,
	}
	ch.Tags = tags
	ch.RetryAfter = retryAfter
	if enabled != nil {
		updates["enabled"] = *enabled
		ch.Enabled = *enabled
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	channelCache.Set(id, ch)
	return nil
}

func ChannelAutoDisable(id int, tags []model.ChannelTag, ctx context.Context) error {
	days, _ := SettingGetInt(model.SettingKeyAutoDisableRetryDays)
	if days <= 0 {
		days = 1
	}
	enabled := false
	return ChannelSetTags(id, tags, time.Now().Add(time.Duration(days)*24*time.Hour).Unix(), &enabled, ctx)
}

func ChannelRetryAutoDisabled(ctx context.Context) error {
	now := time.Now().Unix()
	for _, channel := range channelCache.GetAll() {
		if channel.Enabled || channel.RetryAfter <= 0 || channel.RetryAfter > now || !channelHasTag(channel, model.ChannelTagAutoDisabled) {
			continue
		}
		enabled := true
		if err := ChannelSetTags(channel.ID, nil, 0, &enabled, ctx); err != nil {
			return err
		}
	}
	return nil
}

func ChannelCheckAutoDisable(channelID int, billingIssue bool, ctx context.Context) error {
	channel, ok := channelCache.Get(channelID)
	if !ok || !channel.Enabled || len(channel.Keys) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for _, key := range channel.Keys {
		if !key.Enabled || key.ChannelKey == "" {
			continue
		}
		if key.RetryAfter <= now {
			return nil
		}
	}
	tags := []model.ChannelTag{model.ChannelTagAutoDisabled}
	if billingIssue {
		tags = append(tags, model.ChannelTagBillingIssue)
	}
	return ChannelAutoDisable(channelID, tags, ctx)
}

func channelHasTag(channel model.Channel, tag model.ChannelTag) bool {
	for _, item := range channel.Tags {
		if item == tag {
			return true
		}
	}
	return false
}

func ChannelBaseUrlUpdate(channelID int, baseUrl []model.BaseUrl) error {
	ch, ok := channelCache.Get(channelID)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	// Copy to decouple callers from internal cache storage.
	if baseUrl == nil {
		ch.BaseUrls = nil
	} else {
		cp := make([]model.BaseUrl, len(baseUrl))
		copy(cp, baseUrl)
		ch.BaseUrls = cp
	}
	channelCache.Set(channelID, ch)
	return nil
}

// ChannelKeySaveDB 将运行时更新过的 ChannelKey 缓存写入数据库。
func ChannelKeySaveDB(ctx context.Context) error {
	channelKeyCacheNeedUpdateLock.Lock()
	keyIDs := make([]int, 0, len(channelKeyCacheNeedUpdate))
	for id := range channelKeyCacheNeedUpdate {
		keyIDs = append(keyIDs, id)
	}
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()

	if len(keyIDs) == 0 {
		return nil
	}

	dbConn := db.GetDB().WithContext(ctx)
	for _, id := range keyIDs {
		k, ok := channelKeyCache.Get(id)
		if !ok {
			continue
		}
		if err := dbConn.Save(&k).Error; err != nil {
			return err
		}
	}
	return nil
}

func ChannelUpdate(req *model.ChannelUpdateRequest, ctx context.Context) (*model.Channel, error) {
	oldChannel, ok := channelCache.Get(req.ID)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	candidateBaseURLs := oldChannel.BaseUrls
	if req.BaseUrls != nil {
		candidateBaseURLs = *req.BaseUrls
	}
	candidateKeys := make([]model.ChannelKey, 0, len(oldChannel.Keys)+len(req.KeysToAdd))
	deleteIDs := make(map[int]struct{}, len(req.KeysToDelete))
	for _, id := range req.KeysToDelete {
		deleteIDs[id] = struct{}{}
	}
	updateByID := make(map[int]model.ChannelKeyUpdateRequest, len(req.KeysToUpdate))
	for _, ku := range req.KeysToUpdate {
		updateByID[ku.ID] = ku
	}
	for _, key := range oldChannel.Keys {
		if _, ok := deleteIDs[key.ID]; ok {
			continue
		}
		if ku, ok := updateByID[key.ID]; ok && ku.ChannelKey != nil {
			key.ChannelKey = *ku.ChannelKey
		}
		if ku, ok := updateByID[key.ID]; ok && ku.Enabled != nil {
			key.Enabled = *ku.Enabled
		}
		candidateKeys = append(candidateKeys, key)
	}
	for _, key := range req.KeysToAdd {
		candidateKeys = append(candidateKeys, model.ChannelKey{Enabled: key.Enabled, ChannelKey: key.ChannelKey})
	}
	if err := validateChannelDuplicates(req.ID, candidateBaseURLs, candidateKeys); err != nil {
		return nil, err
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var selectFields []string
	updates := model.Channel{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = *req.Name
	}
	if req.Type != nil {
		selectFields = append(selectFields, "type")
		updates.Type = *req.Type
	}
	if req.Enabled != nil {
		selectFields = append(selectFields, "enabled")
		updates.Enabled = *req.Enabled
	}
	if req.Tags != nil {
		selectFields = append(selectFields, "tags")
		updates.Tags = *req.Tags
	}
	if req.RetryAfter != nil {
		selectFields = append(selectFields, "retry_after")
		updates.RetryAfter = *req.RetryAfter
	}
	if req.BaseUrls != nil {
		selectFields = append(selectFields, "base_urls")
		updates.BaseUrls = *req.BaseUrls
	}
	if req.Model != nil {
		selectFields = append(selectFields, "model")
		updates.Model = *req.Model
	}
	if req.CustomModel != nil {
		selectFields = append(selectFields, "custom_model")
		updates.CustomModel = *req.CustomModel
	}
	if req.Proxy != nil {
		selectFields = append(selectFields, "proxy")
		updates.Proxy = *req.Proxy
	}
	if req.AutoSync != nil {
		selectFields = append(selectFields, "auto_sync")
		updates.AutoSync = *req.AutoSync
	}
	if req.AutoGroup != nil {
		selectFields = append(selectFields, "auto_group")
		updates.AutoGroup = *req.AutoGroup
	}
	if req.CustomHeader != nil {
		selectFields = append(selectFields, "custom_header")
		updates.CustomHeader = *req.CustomHeader
	}
	if req.ChannelProxy != nil {
		selectFields = append(selectFields, "channel_proxy")
		updates.ChannelProxy = req.ChannelProxy
	}
	if req.ParamOverride != nil {
		selectFields = append(selectFields, "param_override")
		updates.ParamOverride = req.ParamOverride
	}
	if req.MatchRegex != nil {
		selectFields = append(selectFields, "match_regex")
		updates.MatchRegex = req.MatchRegex
	}

	// 只有当有字段需要更新时才执行 UPDATE
	if len(selectFields) > 0 {
		if err := tx.Model(&model.Channel{}).Where("id = ?", req.ID).Select(selectFields).Updates(&updates).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update channel: %w", err)
		}
	}

	// 删除 keys
	if len(req.KeysToDelete) > 0 {
		if err := tx.Where("id IN ? AND channel_id = ?", req.KeysToDelete, req.ID).Delete(&model.ChannelKey{}).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to delete channel keys: %w", err)
		}
	}

	// 更新 keys（逐条，只更新提供的字段）
	if len(req.KeysToUpdate) > 0 {
		for _, ku := range req.KeysToUpdate {
			updates := map[string]interface{}{}
			if ku.Enabled != nil {
				updates["enabled"] = *ku.Enabled
			}
			if ku.ChannelKey != nil {
				updates["channel_key"] = *ku.ChannelKey
			}
			if ku.Remark != nil {
				updates["remark"] = *ku.Remark
			}
			if len(updates) == 0 {
				continue
			}
			if err := tx.Model(&model.ChannelKey{}).
				Where("id = ? AND channel_id = ?", ku.ID, req.ID).
				Updates(updates).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to update channel key %d: %w", ku.ID, err)
			}
		}
	}

	// 新增 keys
	if len(req.KeysToAdd) > 0 {
		newKeys := make([]model.ChannelKey, 0, len(req.KeysToAdd))
		for _, ka := range req.KeysToAdd {
			newKeys = append(newKeys, model.ChannelKey{
				ChannelID:  req.ID,
				Enabled:    ka.Enabled,
				ChannelKey: ka.ChannelKey,
				Remark:     ka.Remark,
			})
		}
		if err := tx.Create(&newKeys).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create channel keys: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 刷新缓存并返回最新数据
	if err := channelRefreshCacheByID(req.ID, ctx); err != nil {
		return nil, err
	}

	channel, _ := channelCache.Get(req.ID)
	return &channel, nil
}

func ChannelEnabled(id int, enabled bool, ctx context.Context) error {
	oldChannel, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	updates := map[string]interface{}{"enabled": enabled}
	if enabled {
		updates["tags"] = []model.ChannelTag(nil)
		updates["retry_after"] = int64(0)
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	oldChannel.Enabled = enabled
	if enabled {
		oldChannel.Tags = nil
		oldChannel.RetryAfter = 0
	}
	channelCache.Set(id, oldChannel)
	return nil
}

func ChannelDel(id int, ctx context.Context) error {
	ch, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}

	// 开启事务
	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取所有受影响的 GroupID，用于刷新缓存
	var affectedGroupIDs []int
	if err := tx.Model(&model.GroupItem{}).
		Where("channel_id = ?", id).
		Pluck("group_id", &affectedGroupIDs).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to get affected groups: %w", err)
	}

	// 删除所有引用该渠道的 GroupItem
	if err := tx.Where("channel_id = ?", id).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	// 删除渠道 keys
	if err := tx.Where("channel_id = ?", id).Delete(&model.ChannelKey{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel keys: %w", err)
	}

	// 删除统计数据
	if err := tx.Where("channel_id = ?", id).Delete(&model.StatsChannel{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel stats: %w", err)
	}

	// 删除渠道
	if err := tx.Delete(&model.Channel{}, id).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 删除缓存
	channelCache.Del(id)
	for _, k := range ch.Keys {
		if k.ID != 0 {
			channelKeyCache.Del(k.ID)
		}
	}
	StatsChannelDel(id)

	// 刷新受影响的分组缓存
	for _, groupID := range affectedGroupIDs {
		if err := groupRefreshCacheByID(groupID, ctx); err != nil {
			log.Warnf("failed to refresh group cache for group %d: %v", groupID, err)
		}
	}

	return nil
}

func ChannelLLMList(ctx context.Context) ([]model.LLMChannel, error) {
	models := []model.LLMChannel{}
	for _, channel := range channelCache.GetAll() {
		modelNames := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
		for _, modelName := range modelNames {
			if modelName == "" {
				continue
			}
			models = append(models, model.LLMChannel{
				Name:        modelName,
				Enabled:     channel.Enabled,
				ChannelID:   channel.ID,
				ChannelName: channel.Name,
			})
		}
	}
	return models, nil
}

func ChannelGet(id int, ctx context.Context) (*model.Channel, error) {
	channel, ok := channelCache.Get(id)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	return &channel, nil
}

func channelRefreshCache(ctx context.Context) error {
	channels := []model.Channel{}
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		Find(&channels).Error; err != nil {
		log.Warnf("failed to get channels: %v", err)
		return err
	}
	channelKeyCache.Clear()
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()
	for _, channel := range channels {
		channelCache.Set(channel.ID, channel)
		for _, k := range channel.Keys {
			if k.ID != 0 {
				channelKeyCache.Set(k.ID, k)
			}
		}
	}
	return nil
}

func channelRefreshCacheByID(id int, ctx context.Context) error {
	if old, ok := channelCache.Get(id); ok {
		for _, k := range old.Keys {
			if k.ID != 0 {
				channelKeyCache.Del(k.ID)
			}
		}
	}
	var channel model.Channel
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		First(&channel, id).Error; err != nil {
		return err
	}
	channelCache.Set(channel.ID, channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	return nil
}
