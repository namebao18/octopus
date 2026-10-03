package openai

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func strPtr(s string) *string { return &s }

// TestEnsureMessageContent 覆盖「空 content 导致上游 422」这一整类问题。
//
// 背景：历史会话里模型调用工具但无文本、或工具结果为空时，本地转换会得到空
// MessageContent，序列化成 null 或被 omitzero 省略，严格上游（opendesign、
// deepseek 官方）会返回 422（missing field `content` / content should be a
// string or a list）。ensureMessageContent 在出站前统一补成 ""。
func TestEnsureMessageContent(t *testing.T) {
	empty := ""

	t.Run("空 content 被补成空串", func(t *testing.T) {
		messages := []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("hi")}},
			// assistant 调用工具但无文本
			{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "c1", Type: "function"}}},
			// 工具结果为空
			{Role: "tool", ToolCallID: strPtr("c1")},
			// 显式空串
			{Role: "assistant", Content: model.MessageContent{Content: &empty}},
		}

		ensureMessageContent(messages)

		for i, m := range messages {
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("消息 %d 序列化失败: %v", i, err)
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				t.Fatalf("消息 %d 反序列化失败: %v (%s)", i, err, raw)
			}
			content, ok := obj["content"]
			if !ok {
				t.Errorf("消息 %d 序列化后缺少 content 字段: %s", i, raw)
				continue
			}
			if string(content) == "null" {
				t.Errorf("消息 %d 序列化后 content 为 null: %s", i, raw)
				continue
			}
			if string(content) != `""` {
				t.Errorf("消息 %d 期望 content 为空串，实得 %s: %s", i, content, raw)
			}
		}
	})

	t.Run("非空内容保持不变", func(t *testing.T) {
		messages := []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("hello world")}},
			// 多模态（只有图片、无文本）必须保持原样，不能被改成空串
			{Role: "user", Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
				{Type: "image_url"},
			}}},
		}

		ensureMessageContent(messages)

		raw, _ := json.Marshal(messages[0])
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(raw, &obj)
		if string(obj["content"]) != `"hello world"` {
			t.Errorf("文本内容被改动: %s", raw)
		}

		if len(messages[1].Content.MultipleContent) != 1 || messages[1].Content.Content != nil {
			t.Errorf("多模态内容被改动: %+v", messages[1].Content)
		}
	})
}
