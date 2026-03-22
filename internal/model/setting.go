package model

import (
	"fmt"
	"net/url"
	"strconv"
)

type SettingKey string

const (
	SettingKeyProxyURL                SettingKey = "proxy_url"
	SettingKeyStatsSaveInterval       SettingKey = "stats_save_interval"        // 将统计信息写入数据库的周期(分钟)
	SettingKeyModelInfoUpdateInterval SettingKey = "model_info_update_interval" // 模型信息更新间隔(小时)
	SettingKeySyncLLMInterval         SettingKey = "sync_llm_interval"          // LLM 同步间隔(小时)
	SettingKeyRelayLogKeepPeriod      SettingKey = "relay_log_keep_period"      // 日志保存时间范围(天)
	SettingKeyRelayLogKeepEnabled     SettingKey = "relay_log_keep_enabled"     // 是否保留历史日志
	SettingKeyRelayLogMaxRows         SettingKey = "relay_log_max_rows"         // 最多保留多少条 relay logs（0=不限制）
	SettingKeyRelayLogMaxContentBytes SettingKey = "relay_log_max_content_bytes" // 单条日志 request/response 最多保存多少字节（0=不限制）
	SettingKeyRelayLogVacuumInterval  SettingKey = "relay_log_vacuum_interval"  // SQLite 进行 checkpoint/vacuum 的周期(小时, 0=关闭)
	SettingKeyCORSAllowOrigins        SettingKey = "cors_allow_origins"         // 跨域白名单(逗号分隔, 如 "example.com,example2.com"). 为空不允许跨域, "*"允许所有

	// Channel key maintenance.
	SettingKeyChannelKeyAutoDisableEnabled SettingKey = "channel_key_auto_disable_enabled" // 是否在运行时自动禁用明显不可用的 key（如 401/403/402）
	SettingKeyChannelKeyRecheckInterval    SettingKey = "channel_key_recheck_interval"     // 重新检测 auto-disabled key 的周期(分钟)
	SettingKeyChannelKeySaveInterval       SettingKey = "channel_key_save_interval"        // 将运行时更新的 ChannelKey 写入数据库的周期(分钟)
)

const (
	SettingValueTrue  = "true"
	SettingValueFalse = "false"
)

func isBoolSettingValue(s string) bool {
	return s == SettingValueTrue || s == SettingValueFalse
}

func isIntSettingValue(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func isNonNegativeIntSettingValue(s string) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= 0
}

func isPositiveIntSettingValue(s string) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n > 0
}

func isValidProxyURLValue(s string) error {
	if s == "" {
		return nil
	}
	parsedURL, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("proxy URL is invalid: %w", err)
	}
	validSchemes := map[string]bool{
		"http":  true,
		"https": true,
		"socks": true,
	}
	if !validSchemes[parsedURL.Scheme] {
		return fmt.Errorf("proxy URL scheme must be http, https, or socks")
	}
	if parsedURL.Host == "" {
		return fmt.Errorf("proxy URL must have a host")
	}
	return nil
}

type Setting struct {
	Key   SettingKey `json:"key" gorm:"primaryKey"`
	Value string     `json:"value" gorm:"not null"`
}

func DefaultSettings() []Setting {
	return []Setting{
		{Key: SettingKeyProxyURL, Value: ""},
		{Key: SettingKeyStatsSaveInterval, Value: "10"},       // 默认10分钟保存一次统计信息
		{Key: SettingKeyCORSAllowOrigins, Value: ""},          // CORS 默认不允许跨域，设置为 "*" 才允许所有来源
		{Key: SettingKeyModelInfoUpdateInterval, Value: "24"}, // 默认24小时更新一次模型信息
		{Key: SettingKeySyncLLMInterval, Value: "24"},         // 默认24小时同步一次LLM
		{Key: SettingKeyRelayLogKeepPeriod, Value: "7"},       // 默认日志保存7天
		{Key: SettingKeyRelayLogKeepEnabled, Value: SettingValueTrue},
		{Key: SettingKeyRelayLogMaxRows, Value: "50000"},
		{Key: SettingKeyRelayLogMaxContentBytes, Value: "32768"},
		{Key: SettingKeyRelayLogVacuumInterval, Value: "24"},

		{Key: SettingKeyChannelKeyAutoDisableEnabled, Value: SettingValueTrue},
		{Key: SettingKeyChannelKeyRecheckInterval, Value: "60"},
		{Key: SettingKeyChannelKeySaveInterval, Value: "10"},
	}
}

func (s *Setting) Validate() error {
	switch s.Key {
	case SettingKeyModelInfoUpdateInterval, SettingKeySyncLLMInterval, SettingKeyRelayLogKeepPeriod:
		if !isIntSettingValue(s.Value) {
			return fmt.Errorf("%s must be an integer", s.Key)
		}
		return nil
	case SettingKeyStatsSaveInterval, SettingKeyChannelKeyRecheckInterval, SettingKeyChannelKeySaveInterval:
		if !isPositiveIntSettingValue(s.Value) {
			return fmt.Errorf("%s must be a positive integer", s.Key)
		}
		return nil
	case SettingKeyRelayLogMaxRows, SettingKeyRelayLogMaxContentBytes, SettingKeyRelayLogVacuumInterval:
		if !isNonNegativeIntSettingValue(s.Value) {
			return fmt.Errorf("%s must be a non-negative integer", s.Key)
		}
		return nil
	case SettingKeyRelayLogKeepEnabled, SettingKeyChannelKeyAutoDisableEnabled:
		if !isBoolSettingValue(s.Value) {
			return fmt.Errorf("%s must be true or false", s.Key)
		}
		return nil
	case SettingKeyProxyURL:
		return isValidProxyURLValue(s.Value)
	}

	return nil
}
