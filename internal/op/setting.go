package op

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"gorm.io/gorm"
)

var settingCache = cache.New[model.SettingKey, string](16)

func SettingList(ctx context.Context) ([]model.Setting, error) {
	settings := make([]model.Setting, 0, settingCache.Len())
	for key, value := range settingCache.GetAll() {
		if key.IsInternal() {
			continue
		}
		settings = append(settings, model.Setting{
			Key:   key,
			Value: value,
		})
	}
	return settings, nil
}

func SettingGetString(key model.SettingKey) (string, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return "", fmt.Errorf("setting not found")
	}
	return setting, nil
}

func SettingSetString(key model.SettingKey, value string) error {
	valueCache, ok := settingCache.Get(key)
	if !ok {
		return fmt.Errorf("setting not found")
	}
	if valueCache == value {
		return nil
	}
	result := db.GetDB().Model(&model.Setting{Key: key}).Update("Value", value)
	if result.Error != nil {
		return fmt.Errorf("failed to set setting: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("failed to set setting, key not found")
	}
	settingCache.Set(key, value)
	return nil
}

func SettingGetInt(key model.SettingKey) (int, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return 0, fmt.Errorf("setting not found")
	}
	return strconv.Atoi(setting)
}

func SettingGetBool(key model.SettingKey) (bool, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return false, fmt.Errorf("setting not found")
	}
	return strconv.ParseBool(setting)
}

func SettingSetInt(key model.SettingKey, value int) error {
	valueCache, ok := settingCache.Get(key)
	if !ok {
		return fmt.Errorf("setting not found")
	}
	valueCacheNum, err := strconv.Atoi(valueCache)
	if err != nil {
		return fmt.Errorf("failed to set setting: %w", err)
	}
	if valueCacheNum == value {
		return nil
	}
	result := db.GetDB().Model(&model.Setting{Key: key}).Update("Value", value)
	if result.Error != nil {
		return fmt.Errorf("failed to set setting: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("failed to set setting, key not found")
	}
	settingCache.Set(key, strconv.Itoa(value))
	return nil
}

func settingRefreshCache(ctx context.Context) error {
	db := db.GetDB().WithContext(ctx)

	var settings []model.Setting
	if err := db.Find(&settings).Error; err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	existingKeys := make(map[model.SettingKey]bool)
	for _, setting := range settings {
		existingKeys[setting.Key] = true
	}

	defaultSettings := model.DefaultSettings()
	missingSettings := make([]model.Setting, 0, len(defaultSettings))

	for _, defaultSetting := range defaultSettings {
		if !existingKeys[defaultSetting.Key] {
			missingSettings = append(missingSettings, defaultSetting)
		}
	}

	if len(missingSettings) > 0 {
		if err := db.CreateInBatches(missingSettings, len(missingSettings)).Error; err != nil {
			return fmt.Errorf("failed to create missing settings: %w", err)
		}
		settings = append(settings, missingSettings...)
	}
	for _, setting := range settings {
		settingCache.Set(setting.Key, setting.Value)
	}
	if err := ensureJWTSecret(db); err != nil {
		return fmt.Errorf("failed to ensure jwt secret: %w", err)
	}
	return nil
}

// ensureJWTSecret generates a persistent random JWT secret on first startup
// and stores it in the settings table. Subsequent startups reuse the value
// so existing tokens remain valid until explicitly rotated.
func ensureJWTSecret(tx *gorm.DB) error {
	var existing model.Setting
	err := tx.Where("key = ?", model.SettingKeyJWTSecret).First(&existing).Error
	if err == nil {
		settingCache.Set(model.SettingKeyJWTSecret, existing.Value)
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	secret, err := generateJWTSecret()
	if err != nil {
		return err
	}
	row := model.Setting{Key: model.SettingKeyJWTSecret, Value: secret}
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	settingCache.Set(model.SettingKeyJWTSecret, secret)
	return nil
}

func generateJWTSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate jwt secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// JWTSecret returns the current JWT signing secret. Callers should treat
// the returned bytes as opaque and never log them.
func JWTSecret() ([]byte, error) {
	value, ok := settingCache.Get(model.SettingKeyJWTSecret)
	if !ok || value == "" {
		return nil, fmt.Errorf("jwt secret not initialised")
	}
	return []byte(value), nil
}

// RotateJWTSecret replaces the JWT secret with a freshly generated value,
// invalidating every previously issued token.
func RotateJWTSecret() error {
	secret, err := generateJWTSecret()
	if err != nil {
		return err
	}
	result := db.GetDB().Model(&model.Setting{Key: model.SettingKeyJWTSecret}).Update("Value", secret)
	if result.Error != nil {
		return fmt.Errorf("failed to rotate jwt secret: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		row := model.Setting{Key: model.SettingKeyJWTSecret, Value: secret}
		if err := db.GetDB().Create(&row).Error; err != nil {
			return fmt.Errorf("failed to rotate jwt secret: %w", err)
		}
	}
	settingCache.Set(model.SettingKeyJWTSecret, secret)
	return nil
}
