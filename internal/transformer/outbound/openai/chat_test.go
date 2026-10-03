package openai

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func strPtr(s string) *string { return &s }

// contentRaw 返回消息序列化后 content 字段的原始 JSON。
// 字段缺失（被 omitzero 省略）时返回 "<MISSING>"，content 为 null 时返回 "null"。
func contentRaw(t *testing.T, m model.Message) string {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("反序列化失败: %v (%s)", err, raw)
	}
	c, ok := obj["content"]
	if !ok {
		return "<MISSING>"
	}
	return string(c)
}

// TestEnsureMessageContent 覆盖「空 content 导致上游 422」这一整类问题。
//
// 背景：历史会话里模型调用工具但无文本、或工具结果为空时，本地转换会得到空
// MessageContent，序列化成 null 或被 omitzero 省略，严格上游（opendesign、
// deepseek 官方）会返回 422（missing field `content` / content should be a
// string or a list）。ensureMessageContent 在出站前统一补成 ""。
func TestEnsureMessageContent(t *testing.T) {
	empty := ""

	t.Run("空内容一律补成空串", func(t *testing.T) {
		messages := []model.Message{
			// assistant 调用工具但无文本
			{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "c1", Type: "function"}}},
			// 工具结果为空
			{Role: "tool", ToolCallID: strPtr("c1")},
			// 显式空串
			{Role: "assistant", Content: model.MessageContent{Content: &empty}},
			// 空的多模态数组
			{Role: "assistant", Content: model.MessageContent{MultipleContent: []model.MessageContentPart{}}},
		}

		ensureMessageContent(messages)

		for i := range messages {
			if got := contentRaw(t, messages[i]); got != `""` {
				t.Errorf("消息 %d 期望 content=\"\"，实得 %s", i, got)
			}
		}
	})

	t.Run("非空内容保持不变", func(t *testing.T) {
		messages := []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("hello world")}},
			// 只有图片、无文本的多模态内容必须保持原样，不能被改成空串
			{Role: "user", Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
				{Type: "image_url"},
			}}},
		}

		ensureMessageContent(messages)

		if got := contentRaw(t, messages[0]); got != `"hello world"` {
			t.Errorf("文本内容被改动: %s", got)
		}
		if messages[1].Content.Content != nil || len(messages[1].Content.MultipleContent) != 1 {
			t.Errorf("多模态内容被改动: %+v", messages[1].Content)
		}
	})
}
