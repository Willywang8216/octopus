package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const (
	TaskPriceUpdate   = "price_update"
	TaskStatsSave     = "stats_save"
	TaskRelayLogSave  = "relay_log_save"
	TaskSyncLLM       = "sync_llm"
	TaskCleanLLM      = "clean_llm"
	TaskBaseUrlDelay  = "base_url_delay"
	TaskBalancerGC    = "balancer_gc"
	TaskChannelProbe  = "channel_probe"
)

// 平衡器内存维护：每 5 分钟清理一次过期会话和空闲熔断器条目。
const (
	balancerGCInterval     = 5 * time.Minute
	stickySessionMaxAge    = 24 * time.Hour
	circuitBreakerIdleTime = 24 * time.Hour
)

func Init() {
	priceUpdateIntervalHours, err := op.SettingGetInt(model.SettingKeyModelInfoUpdateInterval)
	if err != nil {
		log.Errorf("failed to get model info update interval: %v", err)
		return
	}
	priceUpdateInterval := time.Duration(priceUpdateIntervalHours) * time.Hour
	// 注册价格更新任务
	Register(string(model.SettingKeyModelInfoUpdateInterval), priceUpdateInterval, true, func() {
		if err := price.UpdateLLMPrice(context.Background()); err != nil {
			log.Warnf("failed to update price info: %v", err)
		}
	})

	// 注册基础URL延迟任务
	Register(TaskBaseUrlDelay, 1*time.Hour, true, ChannelBaseUrlDelayTask)

	// 注册LLM同步任务
	syncLLMIntervalHours, err := op.SettingGetInt(model.SettingKeySyncLLMInterval)
	if err != nil {
		log.Warnf("failed to get sync LLM interval: %v", err)
		return
	}
	syncLLMInterval := time.Duration(syncLLMIntervalHours) * time.Hour
	Register(string(model.SettingKeySyncLLMInterval), syncLLMInterval, true, SyncModelsTask)

	// 注册统计保存任务
	statsSaveIntervalMinutes, err := op.SettingGetInt(model.SettingKeyStatsSaveInterval)
	if err != nil {
		log.Warnf("failed to get stats save interval: %v", err)
		return
	}
	statsSaveInterval := time.Duration(statsSaveIntervalMinutes) * time.Minute
	Register(TaskStatsSave, statsSaveInterval, false, op.StatsSaveDBTask)

	// 注册 ChannelKey 保存任务（将运行时更新写入数据库）
	channelKeySaveIntervalMinutes, err := op.SettingGetInt(model.SettingKeyChannelKeySaveInterval)
	if err != nil {
		log.Warnf("failed to get channel key save interval: %v", err)
		return
	}
	Register(TaskChannelKeySave, time.Duration(channelKeySaveIntervalMinutes)*time.Minute, false, func() {
		if err := op.ChannelKeySaveDB(context.Background()); err != nil {
			log.Warnf("channel key save db task failed: %v", err)
		}
	})

	// 注册中继日志保存任务
	Register(TaskRelayLogSave, 10*time.Minute, false, func() {
		if err := op.RelayLogSaveDBTask(context.Background()); err != nil {
			log.Warnf("relay log save db task failed: %v", err)
		}
	})

	// 注册平衡器内存维护任务，回收过期的粘性会话和空闲熔断器条目，
	// 防止 sync.Map 在长时间运行的实例中无限增长。
	Register(TaskBalancerGC, balancerGCInterval, false, func() {
		sessions := balancer.GCSticky(stickySessionMaxAge)
		circuits := balancer.GCCircuit(circuitBreakerIdleTime)
		if sessions > 0 || circuits > 0 {
			log.Debugf("balancer GC removed %d sticky sessions, %d circuit entries", sessions, circuits)
		}
	})

	// 注册渠道健康探测任务：周期性向非 ALIVE 渠道发送最小请求，
	// 配合 scripts/auditChannels.py 让 NEW/FLAKY 渠道更快被分类。
	probeIntervalMinutes, err := op.SettingGetInt(model.SettingKeyChannelProbeInterval)
	if err != nil {
		log.Warnf("failed to get channel probe interval: %v", err)
		return
	}
	Register(TaskChannelProbe, time.Duration(probeIntervalMinutes)*time.Minute, false, ChannelProbeTask)
}
