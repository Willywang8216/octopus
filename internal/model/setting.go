package model

import (
	"fmt"
	"net/url"
	"strconv"
)

type SettingKey string

const (
	SettingKeyProxyURL                     SettingKey = "proxy_url"
	SettingKeyStatsSaveInterval            SettingKey = "stats_save_interval"          // 将统计信息写入数据库的周期(分钟)
	SettingKeyModelInfoUpdateInterval      SettingKey = "model_info_update_interval"   // 模型信息更新间隔(小时)
	SettingKeySyncLLMInterval              SettingKey = "sync_llm_interval"            // LLM 同步间隔(小时)
	SettingKeyRelayLogKeepPeriod           SettingKey = "relay_log_keep_period"        // 日志保存时间范围(天)
	SettingKeyRelayLogKeepEnabled          SettingKey = "relay_log_keep_enabled"       // 是否保留历史日志
	SettingKeyCORSAllowOrigins             SettingKey = "cors_allow_origins"           // 跨域白名单(逗号分隔, 如 "example.com,example2.com"). 为空不允许跨域, "*"允许所有
	SettingKeyCircuitBreakerThreshold      SettingKey = "circuit_breaker_threshold"    // 熔断触发阈值（连续失败次数）
	SettingKeyCircuitBreakerCooldown       SettingKey = "circuit_breaker_cooldown"     // 熔断基础冷却时间（秒）
	SettingKeyCircuitBreakerMaxCooldown    SettingKey = "circuit_breaker_max_cooldown" // 熔断最大冷却时间（秒），指数退避上限
	SettingKeyHealthCheckInterval          SettingKey = "health_check_interval"        // 自动重测被禁用key/通道的间隔(分钟)，0=禁用
	SettingKeyHealthCheckProbeTimeout      SettingKey = "health_check_probe_timeout"   // 单次探测超时时间(秒)
	SettingKeyJWTSecret                    SettingKey = "jwt_secret"
	SettingKeyChannelKeyRecheckInterval    SettingKey = "channel_key_recheck_interval"     // key重检间隔(分钟)
	SettingKeyChannelKeySaveInterval       SettingKey = "channel_key_save_interval"        // key状态保存间隔(分钟)
	SettingKeyGroupItemRecheckInterval     SettingKey = "group_item_recheck_interval"      // 分组项重检间隔(分钟)
	SettingKeyRelayLogMaxRows              SettingKey = "relay_log_max_rows"               // 日志最大保留行数，0=不限制
	SettingKeyRelayLogMaxContentBytes      SettingKey = "relay_log_max_content_bytes"      // 单条日志内容最大字节数，0=不限制
	SettingKeyRelayLogVacuumInterval       SettingKey = "relay_log_vacuum_interval"        // 日志清理间隔(小时)，0=禁用
	SettingKeyChannelKeyAutoDisableEnabled SettingKey = "channel_key_auto_disable_enabled" // 是否自动禁用故障key
	SettingKeyAutoDisableRetryHours        SettingKey = "auto_disable_retry_hours"         // 自动禁用通道后的重试间隔(小时)
)

// IsInternal reports whether a setting key holds a server-side secret that
// must not be exposed via the public settings API.
func (k SettingKey) IsInternal() bool {
	switch k {
	case SettingKeyJWTSecret:
		return true
	}
	return false
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
		{Key: SettingKeyHealthCheckInterval, Value: "30"},        // 默认每30分钟自动重测被禁用的key/通道
		{Key: SettingKeyHealthCheckProbeTimeout, Value: "20"},    // 默认单次探测20秒超时
		{Key: SettingKeyChannelKeyRecheckInterval, Value: "10"},
		{Key: SettingKeyChannelKeySaveInterval, Value: "1"},
		{Key: SettingKeyGroupItemRecheckInterval, Value: "10"},
		{Key: SettingKeyRelayLogMaxRows, Value: "100000"},
		{Key: SettingKeyRelayLogMaxContentBytes, Value: "65536"},
		{Key: SettingKeyRelayLogVacuumInterval, Value: "24"},
		{Key: SettingKeyChannelKeyAutoDisableEnabled, Value: "true"},
		{Key: SettingKeyAutoDisableRetryHours, Value: "24"},
	}
}

func (s *Setting) Validate() error {
	switch s.Key {
	case SettingKeyModelInfoUpdateInterval, SettingKeySyncLLMInterval, SettingKeyRelayLogKeepPeriod,
		SettingKeyHealthCheckProbeTimeout:
		if !isPositiveIntSettingValue(s.Value) {
			return fmt.Errorf("%s must be a positive integer", s.Key)
		}
		return nil
	case SettingKeyHealthCheckInterval:
		if !isNonNegativeIntSettingValue(s.Value) {
			return fmt.Errorf("%s must be a non-negative integer", s.Key)
		}
		return nil
	case SettingKeyStatsSaveInterval, SettingKeyChannelKeyRecheckInterval, SettingKeyChannelKeySaveInterval,
		SettingKeyCircuitBreakerThreshold, SettingKeyCircuitBreakerCooldown, SettingKeyCircuitBreakerMaxCooldown,
		SettingKeyGroupItemRecheckInterval, SettingKeyAutoDisableRetryHours:
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

func isPositiveIntSettingValue(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0
}

func isNonNegativeIntSettingValue(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n >= 0
}

func isBoolSettingValue(value string) bool {
	return value == "true" || value == "false"
}

func isValidProxyURLValue(value string) error {
	if value == "" {
		return nil
	}
	parsedURL, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("proxy URL is invalid: %w", err)
	}
	validSchemes := map[string]bool{
		"http":   true,
		"https":  true,
		"socks5": true,
	}
	if !validSchemes[parsedURL.Scheme] {
		return fmt.Errorf("proxy URL scheme must be http, https, socks5")
	}
	if parsedURL.Host == "" {
		return fmt.Errorf("proxy URL must have a host")
	}
	return nil
}
