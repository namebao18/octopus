package relay

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// ==================== 上下文窗口感知路由 ====================
//
// 问题：一个分组（failover）里各渠道/模型上下文窗口差异很大。客户端按「分组声明的
// 窗口」（如 1M）规划上下文，一旦首选渠道熔断回退到小窗口模型（如 33K），请求会
// 超出该模型窗口 → 上游要么拒绝，要么静默截断 → 对话错乱。
//
// 方案：转发前估算请求的输入 token；若某候选渠道的「渠道:模型」窗口装不下，就跳过
// 它继续回退；若所有候选都装不下，明确报错（而不是让上游静默截断）。
//
// 窗口来源：设置项 context_windows（JSON）。无可靠自动源，故手工配置：
//   {"6:Qwen/Qwen3.5-27B": 131072, "1:THUDM/GLM-4-9B-0414": 33792}
// 也支持仅按模型名兜底：{"某模型名": 131072}。
// 未配置 → 不过滤（向后兼容，零行为变化）。

// contextWindow map 解析缓存（按设置原文缓存，避免每请求重复解析）。
var ctxWinCache struct {
	raw string
	m   map[string]int
}

func parseContextWindows() map[string]int {
	raw, err := op.SettingGetString(model.SettingKeyContextWindows)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	if ctxWinCache.raw == raw {
		return ctxWinCache.m
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		// 配置非法时保守返回 nil（不做过滤），避免因配置手误把所有请求挡死
		return nil
	}
	ctxWinCache.raw = raw
	ctxWinCache.m = m
	return m
}

// windowFor 返回某「渠道:模型」的窗口；未配置返回 0（表示「未知/不过滤」）。
// 查找顺序：精确 key「渠道ID:模型名」→ 仅模型名。
func windowFor(windows map[string]int, channelID int, modelName string) int {
	if len(windows) == 0 {
		return 0
	}
	if v, ok := windows[strconv.Itoa(channelID)+":"+modelName]; ok {
		return v
	}
	if v, ok := windows[modelName]; ok {
		return v
	}
	return 0
}

// estimateInputTokens 估算请求的输入 token 数。
// 优先取入站阶段已算好的精确值（EstInputTokens）；
// 缺失时回退为「原始请求体字节数 / 3」的粗估——宁可高估（多跳过）也不低估（放行超限）。
func estimateInputTokens(internalRequest *model.InternalLLMRequest) int64 {
	if internalRequest.EstInputTokens > 0 {
		return internalRequest.EstInputTokens
	}
	if len(internalRequest.RawRequest) > 0 {
		// 粗估：英文约 4 字符/token，中文约 1~2 字符/token，取 3 字符/token 偏保守（高估）
		return int64(len(internalRequest.RawRequest) / 3)
	}
	return 0
}

// filterGroupByContextWindow 过滤分组候选：剔除窗口装不下本次请求的「渠道:模型」。
// 返回过滤后的分组，以及被跳过的条目描述（用于日志/报错）。
func filterGroupByContextWindow(group model.Group, channelName func(int) string, inputTokens int64) (model.Group, []string) {
	windows := parseContextWindows()
	if len(windows) == 0 || inputTokens <= 0 {
		return group, nil
	}
	kept := make([]model.GroupItem, 0, len(group.Items))
	var skipped []string
	for _, item := range group.Items {
		w := windowFor(windows, item.ChannelID, item.ModelName)
		if w > 0 && int64(w) < inputTokens {
			skipped = append(skipped, strings.Join([]string{
				channelName(item.ChannelID), item.ModelName,
			}, "/"))
			continue
		}
		kept = append(kept, item)
	}
	group.Items = kept
	return group, skipped
}
