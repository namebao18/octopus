package relay

import (
	"errors"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// errEmptyStreamShell 表示上游返回了一个"完全空壳"的流式响应块。
// 用于在流式处理中把这种野块与普通 chunk 区分开，以便在首 token 之前触发换渠道重试。
var errEmptyStreamShell = errors.New("empty stream shell")

// isEmptyChatResponse 判断非流式 chat 响应是否为"零输出空回复"。
//
// 背景：部分免费模型（如 gemini-3.5-flash-lite）偶发返回 finish_reason=stop，
// 但既无文本内容也无工具调用的空气响应（HTTP 层面合法成功），
// 导致下游应用（如 MoviePilot Agent）拿到空气后静默不回复。
//
// v0.9.28-emptyfix.5 修正：调用方（relay.go 的 handleResponse）已确认这是 chat 请求，
// 因此这里不能再依赖"响应里有没有 choices"来判定是否 chat 响应——
// 旧写法 `!resp.IsChatResponse()`（即 len(Choices)==0 就放行）恰好把
// "上游返回 0 个 choices 的空壳"这种最该拦截的情况放行了，
// 结果被序列化成 {"id":"","object":"","created":0,"model":""} 交给客户端，
// 触发下游客户端 zod 校验失败（invalid_union: choices 数组 / error 对象均缺失）而中断对话。
//
// 判定规则（保守策略，宁可放过不可误杀）：
// 所有 choice 的 message 同时满足以下条件才视为空：
//   - 无工具调用（tool_calls 为空）
//   - 无文本内容或多模态内容
//   - 无思考/推理内容（reasoning_content / reasoning）
//
// embedding 响应（与 chat 互斥）一律不算空，避免误伤。
// 返回 true 表示这是空回复，调用方应将其视为上游失败以触发换渠道重试。
func isEmptyChatResponse(resp *model.InternalLLMResponse) bool {
	// nil 响应（上游返回空 body 且未报错）视为空回复，交由外层换渠道重试
	if resp == nil {
		return true
	}
	// embedding 响应（与 chat 互斥）不判定为空，避免误伤
	if resp.IsEmbeddingResponse() {
		return false
	}
	// chat 请求却一个 choice 都没有 → 空壳响应，必须判定为空
	if len(resp.Choices) == 0 {
		return true
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

// isEmptyStreamShell 判断流式响应块是否为"完全空壳"。
//
// 与非流式的 isEmptyChatResponse 不同：流式场景下 choices 为空是合法的
// （如 usage-only 尾块），只要该块带有 id / object / model / usage 等任一有效字段就不算空壳。
// 只有所有关键字段全为空的"野块"（如 `data: {}`）才是需要处理的对象——
// 直接透传给下游会触发客户端 zod 校验失败（invalid_union）并中断对话。
func isEmptyStreamShell(resp *model.InternalLLMResponse) bool {
	if resp == nil {
		return false
	}
	// 携带 error 的块交给既有错误处理逻辑，不在此判定
	if resp.Error != nil {
		return false
	}
	// 有任何有效载荷（choices / embedding / usage）都不算空壳
	if len(resp.Choices) > 0 || len(resp.EmbeddingData) > 0 || resp.Usage != nil {
		return false
	}
	// 有任意标识字段也不算空壳（usage-only 尾块、[DONE] 等都会带上 object 或 id）
	if resp.ID != "" || resp.Object != "" || resp.Model != "" || resp.Created != 0 {
		return false
	}
	return true
}
