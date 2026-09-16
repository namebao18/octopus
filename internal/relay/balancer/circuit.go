package balancer

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// CircuitState 熔断器状态
type CircuitState int

const (
	StateClosed   CircuitState = iota // 正常通行
	StateOpen                         // 熔断中，拒绝所有请求
	StateHalfOpen                     // 半开，仅允许单个试探请求
)

// circuitEntry 单个熔断器条目
type circuitEntry struct {
	State               CircuitState
	ConsecutiveFailures int64
	LastFailureTime     time.Time
	TripCount           int // 累计熔断触发次数（用于指数退避）

	// ==== 以下为「确定性错误」快速熔断补丁（2026-09-16）新增 ====
	// Deterministic 标记：该条目曾因「确定性错误」（余额不足/鉴权失败/模型不存在等）
	// 进入长冷却。这类错误重试不会变好，故「一次即熔断 + 长冷却」；
	// 只要之后成功过一次（RecordSuccess）就会被清除。
	Deterministic bool
	// OpenUntil 明确的冷却截止时间（纯确定性错误使用）。
	// 普通网络类错误仍走 TripCount 指数退避，两者互不干扰。
	OpenUntil time.Time

	mu sync.Mutex
}

// 全局熔断器存储
var globalBreaker sync.Map // key: string -> value: *circuitEntry

// circuitKey 生成熔断器键：channelID:channelKeyID:modelName
func circuitKey(channelID, keyID int, modelName string) string {
	return fmt.Sprintf("%d:%d:%s", channelID, keyID, modelName)
}

// getOrCreateEntry 获取或创建熔断器条目
func getOrCreateEntry(key string) *circuitEntry {
	if v, ok := globalBreaker.Load(key); ok {
		return v.(*circuitEntry)
	}
	entry := &circuitEntry{State: StateClosed}
	actual, _ := globalBreaker.LoadOrStore(key, entry)
	return actual.(*circuitEntry)
}

// getThreshold 获取熔断阈值配置
func getThreshold() int64 {
	v, err := op.SettingGetInt(model.SettingKeyCircuitBreakerThreshold)
	if err != nil || v <= 0 {
		return 5
	}
	return int64(v)
}

// getDeterministicCooldown 获取「确定性错误」的冷却时长（秒），默认 1800（30 分钟）
// 之所以给长冷却：余额不足/鉴权失败/模型不存在这类问题在短时间内不会自愈，
// 每次请求都去撞一遍只会白白增加首字延迟（这正是「AI 兜底忽然变笨」的成因之一）。
func getDeterministicCooldown() time.Duration {
	v, err := op.SettingGetInt(model.SettingKeyCircuitBreakerDeterministicCooldown)
	if err != nil || v <= 0 {
		v = 1800
	}
	return time.Duration(v) * time.Second
}

// GetCooldown 获取当前冷却时间（带指数退避）
func GetCooldown(tripCount int) time.Duration {
	base, err := op.SettingGetInt(model.SettingKeyCircuitBreakerCooldown)
	if err != nil || base <= 0 {
		base = 60
	}
	maxCooldown, err := op.SettingGetInt(model.SettingKeyCircuitBreakerMaxCooldown)
	if err != nil || maxCooldown <= 0 {
		maxCooldown = 600
	}

	// 指数退避：baseCooldown * 2^(tripCount-1)
	cooldown := base
	if tripCount > 1 {
		shift := tripCount - 1
		if shift > 20 { // 防止溢出
			shift = 20
		}
		cooldown = base << shift
	}
	if cooldown > maxCooldown {
		cooldown = maxCooldown
	}

	return time.Duration(cooldown) * time.Second
}

// ==== 确定性错误判定补丁（2026-09-16）====

// deterministicStatus 这些 HTTP 状态码代表「重试也不会变好」：
// 401 鉴权失败、402 欠费/余额、403 无权限、404 模型/端点不存在。
// 注意：故意不含 400（可能是我方请求体问题，误判会把整组渠道全部长冷却）、
// 也不含 408/429/5xx（属临时性）。
var deterministicStatus = map[int]bool{401: true, 402: true, 403: true, 404: true}

// deterministicKeywords 有些厂商用 429/500 来表达「余额不足」，
// 因此除了状态码还要看错误文本（如魔搭的 429 + "insufficient balance"、
// 智谱的 429 + "余额不足或无可用资源包"）。
var deterministicKeywords = []string{
	"insufficient balance", "insufficient_quota", "insufficient quota",
	"exceeded your current quota", "quota exceeded", "credit balance",
	"invalid api key", "invalid_api_key", "unauthorized", "authentication failed",
	"余额不足", "无可用资源包", "欠费", "账户余额", "arrears", "billing",
}

// ClassifyFailure 判定一次失败是否为「确定性错误」，并给出简短原因。
// deterministic=true 表示该渠道+密钥+模型应立刻进入长冷却。
func ClassifyFailure(statusCode int, errMsg string) (bool, string) {
	lower := strings.ToLower(errMsg)
	for _, kw := range deterministicKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true, fmt.Sprintf("status=%d keyword=%q", statusCode, kw)
		}
	}
	if deterministicStatus[statusCode] {
		snippet := errMsg
		if r := []rune(snippet); len(r) > 120 {
			snippet = string(r[:120]) + "..."
		}
		return true, fmt.Sprintf("status=%d %s", statusCode, snippet)
	}
	return false, ""
}

// IsTripped 检查通道是否处于熔断状态
// 返回 tripped=true 表示该通道应被跳过，remaining 为剩余冷却时间
func IsTripped(channelID, keyID int, modelName string) (tripped bool, remaining time.Duration) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return false, 0 // 无记录，视为 Closed
	}
	entry := v.(*circuitEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// 优先处理「确定性错误」的显式冷却截止时间
	if !entry.OpenUntil.IsZero() {
		if now := time.Now(); now.Before(entry.OpenUntil) {
			return true, time.Until(entry.OpenUntil)
		}
		// 冷却到期 → 放一个试探请求（半开），成功则恢复，失败则继续熔断
		entry.OpenUntil = time.Time{}
		entry.State = StateHalfOpen
		log.Infof("circuit breaker [%s] deterministic cooldown elapsed -> HalfOpen", key)
		return false, 0
	}

	switch entry.State {
	case StateClosed:
		return false, 0

	case StateOpen:
		cooldown := GetCooldown(entry.TripCount)
		elapsed := time.Since(entry.LastFailureTime)
		if elapsed >= cooldown {
			entry.State = StateHalfOpen
			log.Infof("circuit breaker [%s] Open -> HalfOpen (cooldown %v elapsed)", key, cooldown)
			return false, 0
		}
		// 仍在冷却中
		return true, cooldown - elapsed

	case StateHalfOpen:
		// 已有试探请求在进行中，拒绝其他请求
		return true, 0

	default:
		return false, 0
	}
}

// RecordSuccess 记录成功，重置熔断器状态
func RecordSuccess(channelID, keyID int, modelName string) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return
	}
	entry := v.(*circuitEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State == StateHalfOpen {
		log.Infof("circuit breaker [%s] HalfOpen -> Closed (probe succeeded)", key)
	}

	// 重置全部状态（含确定性错误的标记与显式冷却）
	entry.State = StateClosed
	entry.ConsecutiveFailures = 0
	entry.TripCount = 0
	entry.Deterministic = false
	entry.OpenUntil = time.Time{}
}

// RecordDeterministicFailure 记录「确定性失败」：一次即熔断，并给长冷却。
// statusCode 与 reason 仅用于日志定位（如 429 insufficient balance）。
func RecordDeterministicFailure(channelID, keyID int, modelName string, statusCode int, reason string) {
	key := circuitKey(channelID, keyID, modelName)
	entry := getOrCreateEntry(key)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	cooldown := getDeterministicCooldown()
	entry.State = StateOpen
	entry.Deterministic = true
	entry.LastFailureTime = time.Now()
	entry.OpenUntil = time.Now().Add(cooldown)
	entry.ConsecutiveFailures = 0

	log.Warnf("circuit breaker [%s] deterministic failure (%s) -> Open, cooldown=%v",
		key, reason, cooldown)
}

// RecordFailure 记录失败，可能触发熔断
func RecordFailure(channelID, keyID int, modelName string) {
	key := circuitKey(channelID, keyID, modelName)
	entry := getOrCreateEntry(key)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.LastFailureTime = time.Now()

	switch entry.State {
	case StateClosed:
		// 一旦该条目被标记为确定性错误，后续失败仍按长冷却处理，避免冷却很快缩回
		if entry.Deterministic {
			cooldown := getDeterministicCooldown()
			entry.State = StateOpen
			entry.OpenUntil = time.Now().Add(cooldown)
			log.Warnf("circuit breaker [%s] failure on deterministic entry -> Open, cooldown=%v",
				key, cooldown)
			return
		}
		entry.ConsecutiveFailures++
		threshold := getThreshold()
		if entry.ConsecutiveFailures >= threshold {
			entry.State = StateOpen
			entry.TripCount++
			log.Warnf("circuit breaker [%s] Closed -> Open (failures=%d >= threshold=%d, tripCount=%d, cooldown=%v)",
				key, entry.ConsecutiveFailures, threshold, entry.TripCount, GetCooldown(entry.TripCount))
		}

	case StateHalfOpen:
		entry.ConsecutiveFailures = 0 // 重新开始计数
		if entry.Deterministic {
			cooldown := getDeterministicCooldown()
			entry.State = StateOpen
			entry.OpenUntil = time.Now().Add(cooldown)
			log.Warnf("circuit breaker [%s] deterministic probe failed -> Open, cooldown=%v",
				key, cooldown)
			return
		}
		// 试探失败，重新进入 Open 状态，TripCount 递增（冷却时间翻倍）
		entry.State = StateOpen
		entry.TripCount++
		log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe failed, tripCount=%d, cooldown=%v)",
			key, entry.TripCount, GetCooldown(entry.TripCount))

	case StateOpen:
		// 理论上不应该在 Open 状态下接收到失败记录（请求应被拒绝），
		// 但为安全起见仍更新失败时间
	}
}
