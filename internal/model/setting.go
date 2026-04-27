package model

import (
	"fmt"
	"net/url"
	"strconv"
)

type SettingKey string

const (
	SettingKeyProxyURL                  SettingKey = "proxy_url"
	SettingKeyStatsSaveInterval         SettingKey = "stats_save_interval"          // 将统计信息写入数据库的周期(分钟)
	SettingKeyModelInfoUpdateInterval   SettingKey = "model_info_update_interval"   // 模型信息更新间隔(小时)
	SettingKeySyncLLMInterval           SettingKey = "sync_llm_interval"            // LLM 同步间隔(小时)
	SettingKeyRelayLogKeepPeriod        SettingKey = "relay_log_keep_period"        // 日志保存时间范围(天)
	SettingKeyRelayLogKeepEnabled       SettingKey = "relay_log_keep_enabled"       // 是否保留历史日志
	SettingKeyCORSAllowOrigins          SettingKey = "cors_allow_origins"           // 跨域白名单(逗号分隔, 如 "example.com,example2.com"). 为空不允许跨域, "*"允许所有
	SettingKeyCircuitBreakerThreshold   SettingKey = "circuit_breaker_threshold"    // 熔断触发阈值（连续失败次数）
	SettingKeyCircuitBreakerCooldown    SettingKey = "circuit_breaker_cooldown"     // 熔断基础冷却时间（秒）
	SettingKeyCircuitBreakerMaxCooldown SettingKey = "circuit_breaker_max_cooldown" // 熔断最大冷却时间（秒），指数退避上限
	SettingKeyAutoDisableThreshold      SettingKey = "auto_disable_threshold"       // 连续失败多少次后自动禁用 key
	SettingKeyAutoDisableRetryHours     SettingKey = "auto_disable_retry_hours"     // 自动禁用后多少小时重试
	SettingKeyModelCheckInterval        SettingKey = "model_check_interval"         // 模型可用性检查间隔（小时）
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
		{Key: SettingKeyStatsSaveInterval, Value: "10"},          // 默认10分钟保存一次统计信息
		{Key: SettingKeyCORSAllowOrigins, Value: ""},             // CORS 默认不允许跨域，设置为 "*" 才允许所有来源
		{Key: SettingKeyModelInfoUpdateInterval, Value: "24"},    // 默认24小时更新一次模型信息
		{Key: SettingKeySyncLLMInterval, Value: "24"},            // 默认24小时同步一次LLM
		{Key: SettingKeyRelayLogKeepPeriod, Value: "7"},          // 默认日志保存7天
		{Key: SettingKeyRelayLogKeepEnabled, Value: "true"},      // 默认保留历史日志
		{Key: SettingKeyCircuitBreakerThreshold, Value: "5"},     // 默认连续失败5次触发熔断
		{Key: SettingKeyCircuitBreakerCooldown, Value: "60"},     // 默认基础冷却60秒
		{Key: SettingKeyCircuitBreakerMaxCooldown, Value: "600"}, // 默认最大冷却600秒（10分钟）
		{Key: SettingKeyAutoDisableThreshold, Value: "10"},       // 默认连续失败10次自动禁用
		{Key: SettingKeyAutoDisableRetryHours, Value: "24"},      // 默认24小时后重试
		{Key: SettingKeyModelCheckInterval, Value: "24"},         // 默认24小时检查一次模型可用性
	}
}

func (s *Setting) Validate() error {
	switch s.Key {
	case SettingKeyModelInfoUpdateInterval, SettingKeySyncLLMInterval, SettingKeyRelayLogKeepPeriod,
		SettingKeyCircuitBreakerThreshold, SettingKeyCircuitBreakerCooldown, SettingKeyCircuitBreakerMaxCooldown,
		SettingKeyAutoDisableThreshold, SettingKeyAutoDisableRetryHours, SettingKeyModelCheckInterval:
		_, err := strconv.Atoi(s.Value)
		if err != nil {
			return fmt.Errorf("model info update interval must be an integer")
		}
		return nil
	case SettingKeyStatsSaveInterval, SettingKeyChannelKeyRecheckInterval, SettingKeyChannelKeySaveInterval,
		SettingKeyCircuitBreakerThreshold, SettingKeyCircuitBreakerCooldown, SettingKeyCircuitBreakerMaxCooldown,
		SettingKeyGroupItemRecheckInterval:
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
