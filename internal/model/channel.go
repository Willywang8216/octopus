package model

import (
	"time"

	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// StatusTag constants for channels and keys.
const (
	StatusTagNone              = ""
	StatusTagAutoDisabled      = "auto_disabled"
	StatusTagInsufficientFunds = "insufficient_funds"
	StatusTagQuotaExceeded     = "quota_exceeded"
)

type AutoGroupType int

const (
	AutoGroupTypeNone  AutoGroupType = 0 //不自动分组
	AutoGroupTypeFuzzy AutoGroupType = 1 //模糊匹配
	AutoGroupTypeExact AutoGroupType = 2 //准确匹配
	AutoGroupTypeRegex AutoGroupType = 3 //正则匹配
)

// ChannelTestErrorClass classifies a probe failure so the UI can surface
// "attention needed" tags such as insufficient quota, invalid auth, etc.
type ChannelTestErrorClass string

const (
	ChannelTestErrorNone        ChannelTestErrorClass = ""
	ChannelTestErrorAuth        ChannelTestErrorClass = "auth_invalid"        // 401 / invalid_api_key
	ChannelTestErrorPermission  ChannelTestErrorClass = "permission_denied"   // 403
	ChannelTestErrorQuota       ChannelTestErrorClass = "insufficient_quota"  // 402, balance exhausted, billing issues
	ChannelTestErrorRateLimit   ChannelTestErrorClass = "rate_limited"        // 429
	ChannelTestErrorNotFound    ChannelTestErrorClass = "model_not_found"     // 404 / model_not_found
	ChannelTestErrorBadRequest  ChannelTestErrorClass = "bad_request"         // 400
	ChannelTestErrorServer      ChannelTestErrorClass = "server_error"        // 5xx
	ChannelTestErrorNetwork     ChannelTestErrorClass = "network_error"       // dns / connection / TLS / proxy
	ChannelTestErrorTimeout     ChannelTestErrorClass = "timeout"             // request timed out
	ChannelTestErrorTransform   ChannelTestErrorClass = "transform_error"     // local request build failed
	ChannelTestErrorUnsupported ChannelTestErrorClass = "unsupported_channel" // probe doesn't know how to test this channel type
	ChannelTestErrorOther       ChannelTestErrorClass = "other"
)

// IsAttentionNeeded returns true for failures that should auto-disable a key
// or surface a destructive "attention needed" tag in the UI.
func (c ChannelTestErrorClass) IsAttentionNeeded() bool {
	switch c {
	case ChannelTestErrorAuth, ChannelTestErrorPermission, ChannelTestErrorQuota:
		return true
	}
	return false
}

type Channel struct {
	ID             int                   `json:"id" gorm:"primaryKey"`
	Name           string                `json:"name" gorm:"unique;not null"`
	Type           outbound.OutboundType `json:"type"`
	Enabled        bool                  `json:"enabled" gorm:"default:true"`
	BaseUrls       []BaseUrl             `json:"base_urls" gorm:"serializer:json"`
	Keys           []ChannelKey          `json:"keys" gorm:"foreignKey:ChannelID"`
	Model          string                `json:"model"`
	CustomModel    string                `json:"custom_model"`
	Proxy          bool                  `json:"proxy" gorm:"default:false"`
	AutoSync       bool                  `json:"auto_sync" gorm:"default:false"`
	AutoGroup      AutoGroupType         `json:"auto_group" gorm:"default:0"`
	CustomHeader   []CustomHeader        `json:"custom_header" gorm:"serializer:json"`
	ParamOverride  *string               `json:"param_override"`
	ChannelProxy   *string               `json:"channel_proxy"`
	Stats          *StatsChannel         `json:"stats,omitempty" gorm:"foreignKey:ChannelID"`
	MatchRegex     *string               `json:"match_regex"`
	Tags           []ChannelTag          `json:"tags" gorm:"serializer:json"`
	RetryAfter     int64                 `json:"retry_after" gorm:"default:0"`
	StatusTag      string                `json:"status_tag" gorm:"type:varchar(32);default:''"`
	AutoDisabledAt *int64                `json:"auto_disabled_at"`
	AutoDisabled   bool                  `json:"auto_disabled" gorm:"default:false"`
	DisabledReason string                `json:"disabled_reason" gorm:"type:varchar(64);default:''"`
	DisabledClass  ChannelTestErrorClass `json:"disabled_class" gorm:"type:varchar(32);default:''"`
	DisabledAt     int64                 `json:"disabled_at" gorm:"default:0"`
	LastTestAt     int64                 `json:"last_test_at" gorm:"default:0"`
	AutoDisableThreshold  *int                  `json:"auto_disable_threshold"`
	AutoDisableRetryHours *int                  `json:"auto_disable_retry_hours"`
}

type BaseUrl struct {
	URL   string `json:"url"`
	Delay int    `json:"delay"`
}

type CustomHeader struct {
	HeaderKey   string `json:"header_key"`
	HeaderValue string `json:"header_value"`
}

type ChannelTag string

const (
	ChannelTagAutoDisabled ChannelTag = "auto_disabled"
	ChannelTagBillingIssue ChannelTag = "billing_issue"
)

type ChannelKey struct {
	ID               int                   `json:"id" gorm:"primaryKey"`
	ChannelID        int                   `json:"channel_id"`
	Enabled          bool                  `json:"enabled" gorm:"default:true"`
	ChannelKey       string                `json:"channel_key"`
	StatusCode       int                   `json:"status_code"`
	LastUseTimeStamp int64                 `json:"last_use_time_stamp"`
	TotalCost        float64               `json:"total_cost"`
	Remark           string                `json:"remark"`
	FailureCount     int                   `json:"failure_count" gorm:"default:0"`
	LastError        string                `json:"last_error" gorm:"type:text"`
	RetryAfter      int64                 `json:"retry_after" gorm:"default:0"`
	StatusTag       string                `json:"status_tag" gorm:"type:varchar(32);default:''"`
	AutoDisabled     bool                  `json:"auto_disabled" gorm:"default:false"`
	DisabledReason   string                `json:"disabled_reason" gorm:"type:varchar(64);default:''"`
	DisabledClass    ChannelTestErrorClass `json:"disabled_class" gorm:"type:varchar(32);default:''"`
	DisabledAt       int64                 `json:"disabled_at" gorm:"default:0"`
	LastTestAt       int64                 `json:"last_test_at" gorm:"default:0"`
	LastTestSuccess  int                   `json:"last_test_success" gorm:"default:0"` // # of models that succeeded in last full test
	LastTestFailed   int                   `json:"last_test_failed" gorm:"default:0"`  // # of models that failed in last full test
}

// ChannelTestResult records the most recent test result for a single
// (channel, key, model) triple. It is overwritten each time the user runs a
// test for that combination so the table stays bounded.
type ChannelTestResult struct {
	ID         int                   `json:"id" gorm:"primaryKey"`
	ChannelID  int                   `json:"channel_id" gorm:"index:idx_channel_test_unique,unique;not null"`
	KeyID      int                   `json:"key_id" gorm:"index:idx_channel_test_unique,unique;not null"`
	Model      string                `json:"model" gorm:"index:idx_channel_test_unique,unique;type:varchar(255);not null"`
	Success    bool                  `json:"success"`
	StatusCode int                   `json:"status_code"`
	LatencyMs  int                   `json:"latency_ms"`
	ErrorClass ChannelTestErrorClass `json:"error_class" gorm:"type:varchar(32);default:''"`
	ErrorMsg   string                `json:"error_msg" gorm:"type:text"`
	TestedAt   int64                 `json:"tested_at"`
}

// ChannelUpdateRequest 渠道更新请求 - 仅包含变更的数据
type ChannelUpdateRequest struct {
	ID                    int                    `json:"id" binding:"required"`
	Name                  *string                `json:"name,omitempty"`
	Type                  *outbound.OutboundType `json:"type,omitempty"`
	Enabled               *bool                  `json:"enabled,omitempty"`
	Tags                  *[]ChannelTag          `json:"tags,omitempty"`
	RetryAfter            *int64                 `json:"retry_after,omitempty"`
	BaseUrls              *[]BaseUrl             `json:"base_urls,omitempty"`
	Model                 *string                `json:"model,omitempty"`
	CustomModel           *string                `json:"custom_model,omitempty"`
	Proxy                 *bool                  `json:"proxy,omitempty"`
	AutoSync              *bool                  `json:"auto_sync,omitempty"`
	AutoGroup             *AutoGroupType         `json:"auto_group,omitempty"`
	CustomHeader          *[]CustomHeader        `json:"custom_header,omitempty"`
	ChannelProxy          *string                `json:"channel_proxy,omitempty"`
	ParamOverride         *string                `json:"param_override,omitempty"`
	MatchRegex            *string                `json:"match_regex,omitempty"`
	AutoDisableThreshold  *int                   `json:"auto_disable_threshold,omitempty"`
	AutoDisableRetryHours *int                   `json:"auto_disable_retry_hours,omitempty"`

	KeysToAdd    []ChannelKeyAddRequest    `json:"keys_to_add,omitempty"`
	KeysToUpdate []ChannelKeyUpdateRequest `json:"keys_to_update,omitempty"`
	KeysToDelete []int                     `json:"keys_to_delete,omitempty"`
}

type ChannelKeyAddRequest struct {
	Enabled    bool   `json:"enabled"`
	ChannelKey string `json:"channel_key" binding:"required"`
	Remark     string `json:"remark"`
}

type ChannelKeyUpdateRequest struct {
	ID         int     `json:"id" binding:"required"`
	Enabled    *bool   `json:"enabled,omitempty"`
	ChannelKey *string `json:"channel_key,omitempty"`
	Remark     *string `json:"remark,omitempty"`
}

// ChannelFetchModelRequest is used by /channel/fetch-model (not persisted).
type ChannelFetchModelRequest struct {
	Type    outbound.OutboundType `json:"type" binding:"required"`
	BaseURL string                `json:"base_url" binding:"required"`
	Key     string                `json:"key" binding:"required"`
	Proxy   bool                  `json:"proxy"`
}

func (c *Channel) GetBaseUrl() string {
	if c == nil || len(c.BaseUrls) == 0 {
		return ""
	}

	bestURL := ""
	bestDelay := 0
	bestSet := false

	for _, bu := range c.BaseUrls {
		if bu.URL == "" {
			continue
		}
		if !bestSet || bu.Delay < bestDelay {
			bestURL = bu.URL
			bestDelay = bu.Delay
			bestSet = true
		}
	}

	return bestURL
}

func (c *Channel) GetChannelKey() ChannelKey {
	return c.getChannelKey(0)
}

// GetChannelKeyByID prefers the specified key when it is still available,
// otherwise it falls back to the cheapest healthy key.
func (c *Channel) GetChannelKeyByID(preferredID int) ChannelKey {
	return c.getChannelKey(preferredID)
}

func (c *Channel) getChannelKey(preferredID int) ChannelKey {
	keys := c.GetAvailableKeys()
	if len(keys) == 0 {
		return ChannelKey{}
	}
	if preferredID > 0 {
		for _, k := range keys {
			if k.ID == preferredID {
				return k
			}
		}
	}
	return keys[0]
}

func (c *Channel) GetAvailableKeys() []ChannelKey {
	if c == nil || len(c.Keys) == 0 {
		return nil
	}

	nowSec := time.Now().Unix()
	keys := make([]ChannelKey, 0, len(c.Keys))
	for _, k := range c.Keys {
		if !k.Enabled || k.ChannelKey == "" {
			continue
		}
		if k.StatusCode == 429 && k.LastUseTimeStamp > 0 {
			if nowSec-k.LastUseTimeStamp < int64(5*time.Minute/time.Second) {
				continue
			}
		}
		keys = append(keys, k)
	}

	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j].TotalCost < keys[j-1].TotalCost; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
