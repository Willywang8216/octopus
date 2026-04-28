package balancer

import (
	"math/rand"
	"math"
	"sort"
	"sync/atomic"

	"github.com/bestruirui/octopus/internal/model"
)

var roundRobinCounter uint64

// Balancer 根据负载均衡模式选择通道
type Balancer interface {
	// Candidates 返回按策略排序的候选列表
	// 调用方在遍历候选列表时自行检查熔断状态
	Candidates(items []model.GroupItem) []model.GroupItem
}

// GetBalancer 根据模式返回对应的负载均衡器
func GetBalancer(mode model.GroupMode) Balancer {
	switch mode {
	case model.GroupModeRoundRobin:
		return &RoundRobin{}
	case model.GroupModeRandom:
		return &Random{}
	case model.GroupModeFailover:
		return &Failover{}
	case model.GroupModeWeighted:
		return &Weighted{}
	default:
		return &RoundRobin{}
	}
}

// RoundRobin 轮询：从上次位置开始轮转排列
type RoundRobin struct{}

func (b *RoundRobin) Candidates(items []model.GroupItem) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}
	idx := int(atomic.AddUint64(&roundRobinCounter, 1) % uint64(n))
	result := make([]model.GroupItem, n)
	for i := 0; i < n; i++ {
		result[i] = items[(idx+i)%n]
	}
	return result
}

// Random 随机：随机打乱所有 items
type Random struct{}

func (b *Random) Candidates(items []model.GroupItem) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}
	result := make([]model.GroupItem, n)
	copy(result, items)
	rand.Shuffle(n, func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})
	return result
}

// Failover 故障转移：按优先级排序
type Failover struct{}

func (b *Failover) Candidates(items []model.GroupItem) []model.GroupItem {
	if len(items) == 0 {
		return nil
	}
	return sortByPriority(items)
}

// Weighted 加权分配：按权重概率排序
type Weighted struct{}

func (b *Weighted) Candidates(items []model.GroupItem) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}

	// 构建加权随机排序
	type weightedItem struct {
		item   model.GroupItem
		score  float64
	}

	totalWeight := 0
	for _, item := range items {
		w := item.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}

	scored := make([]weightedItem, n)
	for i, item := range items {
		w := item.Weight
		if w <= 0 {
			w = 1
		}
		// A-Res reservoir-sampling weighted score. Items with higher weight
		// are more likely to receive higher scores.
		// score = U^(1/weight)  where U ~ Uniform(0, 1)
		// Probability that score_i > score_j scales monotonically with weight ratio.
		u := rand.Float64()
		if u <= 0 {
			u = 1e-12
		}
		invW := 1.0 / float64(w)
		scored[i] = weightedItem{
			item:  item,
			score: pow(u, invW),
		}
	}
	_ = totalWeight  // retained for telemetry/future use

	// 按分数降序排列（分数越高优先级越高）
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]model.GroupItem, n)
	for i := range scored {
		result[i] = scored[i].item
	}
	return result
}

func sortByPriority(items []model.GroupItem) []model.GroupItem {
	sorted := make([]model.GroupItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}


// pow is a small wrapper so the formula reads naturally. Keeps a single
// reference to math.Pow for testability.
func pow(x, y float64) float64 {
	return math.Pow(x, y)
}
