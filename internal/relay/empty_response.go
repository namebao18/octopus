package relay

import (
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// isEmptyChatResponse 判断非流式 chat 响应是否为"零输出空回复"。
//
// 背景：部分免费模型（如 gemini-3.5-flash-lite）偶发返回 finish_reason=stop，
// 但既无文本内容也无工具调用的空气响应（HTTP 层面合法成功），
// 导致下游应用（如 MoviePilot Agent）拿到空气后静默不回复。
//
// 判定规则（保守策略，宁可放过不可误杀）：
// 所有 choice 的 message 同时满足以下条件才视为空：
//   - 无工具调用（tool_calls 为空）
//   - 无文本内容或多模态内容
//   - 无思考/推理内容（reasoning_content / reasoning）
//
// 非 chat 响应（embedding、图片生成等没有 choices 的）一律不算空，避免误伤。
// 返回 true 表示这是空回复，调用方应将其视为上游失败以触发换渠道重试。
func isEmptyChatResponse(resp *model.InternalLLMResponse) bool {
	// 非 chat 响应（embedding 等）或结构异常时不判定为空，直接放行
	if resp == nil || !resp.IsChatResponse() {
		return false
	}
	for _, choice := range resp.Choices {
		msg := choice.Message
		// choice 没有 message 字段时跳过该 choice 继续检查其他 choice
		if msg == nil {
			continue
		}
		// 有工具调用 → Agent 工具循环依赖它，绝不算空
		if len(msg.ToolCalls) > 0 {
			return false
		}
		// 有纯文本内容（去除空白后非空）→ 不算空
		if msg.Content.Content != nil && strings.TrimSpace(*msg.Content.Content) != "" {
			return false
		}
		// 有多模态内容（图片/音频/文件列表）→ 不算空
		if len(msg.Content.MultipleContent) > 0 {
			return false
		}
		// 有思考/推理内容 → 不算空（保守处理 reasoning-only 响应）
		if msg.GetReasoningContent() != "" {
			return false
		}
	}
	return true
}
