package model

// ErrorClass categorizes probe failures.
type ErrorClass string

const (
	ErrorClassNone          ErrorClass = ""               // success
	ErrorClassNetwork       ErrorClass = "network"        // DNS / dial / EOF / timeout
	ErrorClassAuthOrQuota   ErrorClass = "auth_or_quota"  // 401/403/429
	ErrorClassUpstreamError ErrorClass = "upstream_error" // 5xx, non-JSON 2xx, parse errors
	ErrorClassOther         ErrorClass = "other"
)

// ChannelHealth is the derived per-channel status (not stored).
type ChannelHealth string

const (
	ChannelHealthUnknown ChannelHealth = "unknown"
	ChannelHealthAlive   ChannelHealth = "alive"
	ChannelHealthFlaky   ChannelHealth = "flaky"
	ChannelHealthZombie  ChannelHealth = "zombie"
	ChannelHealthDead    ChannelHealth = "dead"
)

// ChannelKeyModelStatus is the persisted per-(key,model) probe result.
// Table name: channel_key_model_status. The composite (channel_id, key_id,
// model_name) is unique so probes upsert in place.
type ChannelKeyModelStatus struct {
	ID           int        `json:"id" gorm:"primaryKey"`
	ChannelID    int        `json:"channel_id" gorm:"not null;uniqueIndex:idx_ckms_unique"`
	KeyID        int        `json:"key_id" gorm:"not null;uniqueIndex:idx_ckms_unique"`
	ModelName    string     `json:"model_name" gorm:"size:255;not null;uniqueIndex:idx_ckms_unique"`
	OK           bool       `json:"ok"`
	StatusCode   int        `json:"status_code"`
	LatencyMs    int64      `json:"latency_ms"`
	LastError    string     `json:"last_error" gorm:"type:text"`
	ErrorClass   ErrorClass `json:"error_class"`
	LastTestedAt int64      `json:"last_tested_at"` // unix seconds
}

// ChannelTestSummary is the derived per-channel summary (not stored, computed
// at list-time from the per-(key,model) results).
type ChannelTestSummary struct {
	Total        int           `json:"total"` // total combos tested
	Ok           int           `json:"ok"`
	Failed       int           `json:"failed"`
	LastTestedAt int64         `json:"last_tested_at"` // max over results, 0 if none
	Health       ChannelHealth `json:"health"`
}
