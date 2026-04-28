package op

import (
	"context"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"gorm.io/gorm/clause"
)

var groupCache = cache.New[int, model.Group](16)
var groupMap = cache.New[string, model.Group](16)

func GroupList(ctx context.Context) ([]model.Group, error) {
	groups := make([]model.Group, 0, groupCache.Len())
	for _, group := range groupCache.GetAll() {
		groups = append(groups, group)
	}
	return groups, nil
}

func GroupListModel(ctx context.Context) ([]string, error) {
	models := []string{}
	for _, group := range groupCache.GetAll() {
		models = append(models, group.Name)
	}
	return models, nil
}

func GroupGet(id int, ctx context.Context) (*model.Group, error) {
	group, ok := groupCache.Get(id)
	if !ok {
		return nil, fmt.Errorf("group not found")
	}
	return &group, nil
}

func GroupGetEnabledMap(name string, ctx context.Context) (model.Group, error) {
	group, ok := groupMap.Get(name)
	if !ok {
		return model.Group{}, fmt.Errorf("group not found")
	}
	if len(group.Items) == 0 {
		group.Items = nil
		return group, nil
	}

	enabledItems := make([]model.GroupItem, 0, len(group.Items))
	for _, item := range group.Items {
		if !item.Enabled {
			continue
		}
		channel, ok := channelCache.Get(item.ChannelID)
		if !ok || !channel.Enabled {
			continue
		}
		enabledItems = append(enabledItems, item)
	}
	group.Items = enabledItems
	return group, nil
}

func GroupCreate(group *model.Group, ctx context.Context) error {
	for i := range group.Items {
		group.Items[i].Enabled = true
	}
	if err := db.GetDB().WithContext(ctx).Create(group).Error; err != nil {
		return err
	}
	groupCache.Set(group.ID, *group)
	groupMap.Set(group.Name, *group)
	return nil
}

func GroupCreateCoderPresets(modelName string, ctx context.Context) ([]model.Group, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	targets := []struct {
		groupName string
		regex     string
	}{
		{groupName: "anthropic-claude-code", regex: "^(claude|anthropic|claude-code)$"},
		{groupName: "openai-codex", regex: "^(codex|openai-codex)$"},
	}

	channels, err := ChannelList(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.GroupItem, 0)
	for _, channel := range channels {
		modelNames := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
		for _, name := range modelNames {
			if strings.EqualFold(name, modelName) {
				items = append(items, model.GroupItem{
					ChannelID: channel.ID,
					ModelName: name,
					Weight:    1,
				})
				break
			}
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("model %s is not available in any channel", modelName)
	}

	created := make([]model.Group, 0, len(targets))
	for _, target := range targets {
		if existing, ok := groupMap.Get(target.groupName); ok {
			pairs := make([]model.GroupIDAndLLMName, len(items))
			for i, item := range items {
				pairs[i] = model.GroupIDAndLLMName{ChannelID: item.ChannelID, ModelName: item.ModelName}
			}
			if err := GroupItemBatchAdd(existing.ID, pairs, ctx); err != nil {
				return nil, err
			}
			group, err := GroupGet(existing.ID, ctx)
			if err != nil {
				return nil, err
			}
			created = append(created, *group)
			continue
		}

		groupItems := make([]model.GroupItem, len(items))
		for i, item := range items {
			groupItems[i] = item
			groupItems[i].Priority = i + 1
		}
		group := model.Group{
			Name:              target.groupName,
			Mode:              model.GroupModeFailover,
			MatchRegex:        target.regex,
			FirstTokenTimeOut: 30,
			SessionKeepTime:   0,
			Items:             groupItems,
		}
		if err := GroupCreate(&group, ctx); err != nil {
			return nil, err
		}
		created = append(created, group)
	}
	return created, nil
}

func GroupUpdate(req *model.GroupUpdateRequest, ctx context.Context) (*model.Group, error) {
	oldGroup, ok := groupCache.Get(req.ID)
	if !ok {
		return nil, fmt.Errorf("group not found")
	}
	oldName := oldGroup.Name

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var selectFields []string
	updates := model.Group{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = *req.Name
	}
	if req.Mode != nil {
		selectFields = append(selectFields, "mode")
		updates.Mode = *req.Mode
	}
	if req.MatchRegex != nil {
		selectFields = append(selectFields, "match_regex")
		updates.MatchRegex = *req.MatchRegex
	}
	if req.FirstTokenTimeOut != nil {
		selectFields = append(selectFields, "first_token_time_out")
		updates.FirstTokenTimeOut = *req.FirstTokenTimeOut
	}
	if req.SessionKeepTime != nil {
		selectFields = append(selectFields, "session_keep_time")
		updates.SessionKeepTime = *req.SessionKeepTime
	}

	if len(selectFields) > 0 {
		if err := tx.Model(&model.Group{}).Where("id = ?", req.ID).Select(selectFields).Updates(&updates).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update group: %w", err)
		}
	}

	if len(req.ItemsToDelete) > 0 {
		if err := tx.Where("id IN ? AND group_id = ?", req.ItemsToDelete, req.ID).Delete(&model.GroupItem{}).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to delete items: %w", err)
		}
	}

	// 批量更新 items: 使用参数化的逐条 UPDATE 语句替代手写 CASE 拼接，
	// 防止 priority/weight 字段未来扩展为非整型时引入注入风险。每条
	// 语句仍带 group_id 过滤，与原实现一致地阻止跨组覆盖。
	if len(req.ItemsToUpdate) > 0 {
		for _, item := range req.ItemsToUpdate {
			if err := tx.Model(&model.GroupItem{}).
				Where("id = ? AND group_id = ?", item.ID, req.ID).
				Updates(map[string]interface{}{
					"priority": item.Priority,
					"weight":   item.Weight,
				}).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to update items: %w", err)
			}
		}
	}

	if len(req.ItemsToAdd) > 0 {
		newItems := make([]model.GroupItem, len(req.ItemsToAdd))
		for i, item := range req.ItemsToAdd {
			newItems[i] = model.GroupItem{
				GroupID:   req.ID,
				ChannelID: item.ChannelID,
				ModelName: item.ModelName,
				Priority:  item.Priority,
				Weight:    item.Weight,
				Enabled:   true,
			}
		}
		if err := tx.Create(&newItems).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create items: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	if err := groupRefreshCacheByID(req.ID, ctx); err != nil {
		return nil, err
	}

	group, _ := groupCache.Get(req.ID)
	if oldName != "" && oldName != group.Name {
		groupMap.Del(oldName)
	}
	return &group, nil
}

func GroupDel(id int, ctx context.Context) error {
	group, ok := groupCache.Get(id)
	if !ok {
		return fmt.Errorf("group not found")
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Where("group_id = ?", id).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	if err := tx.Delete(&model.Group{}, id).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	groupCache.Del(id)
	groupMap.Del(group.Name)
	return nil
}

func GroupItemAdd(item *model.GroupItem, ctx context.Context) error {
	if _, ok := groupCache.Get(item.GroupID); !ok {
		return fmt.Errorf("group not found")
	}
	item.Enabled = true

	if err := db.GetDB().WithContext(ctx).Create(item).Error; err != nil {
		return err
	}

	return groupRefreshCacheByID(item.GroupID, ctx)
}

func GroupItemBatchAdd(groupID int, items []model.GroupIDAndLLMName, ctx context.Context) error {
	if len(items) == 0 {
		return nil
	}

	group, ok := groupCache.Get(groupID)
	if !ok {
		return fmt.Errorf("group not found")
	}

	seen := make(map[string]struct{}, len(items))
	uniq := make([]model.GroupIDAndLLMName, 0, len(items))
	for _, it := range items {
		if it.ChannelID == 0 || it.ModelName == "" {
			continue
		}
		k := fmt.Sprintf("%d|%s", it.ChannelID, it.ModelName)
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		uniq = append(uniq, it)
	}
	if len(uniq) == 0 {
		return nil
	}

	nextPriority := 1
	for _, gi := range group.Items {
		if gi.Priority >= nextPriority {
			nextPriority = gi.Priority + 1
		}
	}

	newItems := make([]model.GroupItem, 0, len(uniq))
	for _, it := range uniq {
		newItems = append(newItems, model.GroupItem{
			GroupID:   groupID,
			ChannelID: it.ChannelID,
			ModelName: it.ModelName,
			Priority:  nextPriority,
			Weight:    1,
			Enabled:   true,
		})
		nextPriority++
	}

	if err := db.GetDB().WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "group_id"}, {Name: "channel_id"}, {Name: "model_name"}},
			DoNothing: true,
		}).
		Create(&newItems).Error; err != nil {
		return fmt.Errorf("failed to create group items: %w", err)
	}

	return groupRefreshCacheByID(groupID, ctx)
}

func GroupItemUpdate(item *model.GroupItem, ctx context.Context) error {
	if err := db.GetDB().WithContext(ctx).Model(item).
		Select("ModelName", "Priority", "Weight").
		Updates(item).Error; err != nil {
		return err
	}

	return groupRefreshCacheByID(item.GroupID, ctx)
}

func GroupItemDel(id int, ctx context.Context) error {
	var item model.GroupItem
	if err := db.GetDB().WithContext(ctx).First(&item, id).Error; err != nil {
		return fmt.Errorf("group item not found")
	}

	if err := db.GetDB().WithContext(ctx).Delete(&item).Error; err != nil {
		return err
	}

	return groupRefreshCacheByID(item.GroupID, ctx)
}

func GroupItemDisable(id int, reason string, ctx context.Context) error {
	var item model.GroupItem
	if err := db.GetDB().WithContext(ctx).First(&item, id).Error; err != nil {
		return fmt.Errorf("group item not found")
	}
	if !item.Enabled {
		return nil
	}

	r := strings.TrimSpace(reason)
	if len(r) > 256 {
		r = r[:256]
	}

	prev := strings.TrimSpace(item.DisabledReason)
	msg := fmt.Sprintf("auto-disabled: time=%s reason=%s", time.Now().UTC().Format(time.RFC3339), r)
	if prev != "" {
		msg += " | prev=" + prev
	}

	if err := db.GetDB().WithContext(ctx).Model(&model.GroupItem{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"enabled": false, "disabled_at": time.Now().Unix(), "disabled_reason": msg}).Error; err != nil {
		return err
	}
	return groupRefreshCacheByID(item.GroupID, ctx)
}

func GroupItemEnable(id int, ctx context.Context) error {
	var item model.GroupItem
	if err := db.GetDB().WithContext(ctx).First(&item, id).Error; err != nil {
		return fmt.Errorf("group item not found")
	}
	if item.Enabled {
		return nil
	}

	prev := strings.TrimSpace(item.DisabledReason)
	msg := "auto-reenabled: time=" + time.Now().UTC().Format(time.RFC3339)
	if prev != "" {
		msg += " | prev=" + prev
	}

	if err := db.GetDB().WithContext(ctx).Model(&model.GroupItem{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"enabled": true, "disabled_at": 0, "disabled_reason": msg}).Error; err != nil {
		return err
	}
	return groupRefreshCacheByID(item.GroupID, ctx)
}

// GroupItemBatchDelByChannelAndModels 根据渠道ID和模型名称批量删除分组项
func GroupItemBatchDelByChannelAndModels(keys []model.GroupIDAndLLMName, ctx context.Context) error {
	if len(keys) == 0 {
		return nil
	}

	conditions := make([][]interface{}, len(keys))
	for i, key := range keys {
		conditions[i] = []interface{}{key.ChannelID, key.ModelName}
	}

	var groupIDs []int
	if err := db.GetDB().WithContext(ctx).
		Model(&model.GroupItem{}).
		Distinct("group_id").
		Where("(channel_id, model_name) IN ?", conditions).
		Pluck("group_id", &groupIDs).Error; err != nil {
		return fmt.Errorf("failed to find group ids: %w", err)
	}

	if len(groupIDs) == 0 {
		return nil
	}

	if err := db.GetDB().WithContext(ctx).
		Where("(channel_id, model_name) IN ?", conditions).
		Delete(&model.GroupItem{}).Error; err != nil {
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	if err := groupRefreshCacheByIDs(groupIDs, ctx); err != nil {
		return fmt.Errorf("failed to refresh group cache: %w", err)
	}

	return nil
}

func GroupItemList(groupID int, ctx context.Context) ([]model.GroupItem, error) {
	var items []model.GroupItem
	if err := db.GetDB().WithContext(ctx).
		Where("group_id = ?", groupID).
		Order("priority ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func groupRefreshCache(ctx context.Context) error {
	groups := []model.Group{}
	if err := db.GetDB().WithContext(ctx).
		Preload("Items").
		Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		groupCache.Set(group.ID, group)
		groupMap.Set(group.Name, group)
	}
	return nil
}

func groupRefreshCacheByID(id int, ctx context.Context) error {
	var group model.Group
	if err := db.GetDB().WithContext(ctx).
		Preload("Items").
		First(&group, id).Error; err != nil {
		return err
	}
	groupCache.Set(group.ID, group)
	groupMap.Set(group.Name, group)
	return nil
}

func groupRefreshCacheByIDs(ids []int, ctx context.Context) error {
	if len(ids) == 0 {
		return nil
	}
	var groups []model.Group
	if err := db.GetDB().WithContext(ctx).
		Preload("Items").
		Where("id IN ?", ids).
		Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		groupCache.Set(group.ID, group)
		groupMap.Set(group.Name, group)
	}
	return nil
}
