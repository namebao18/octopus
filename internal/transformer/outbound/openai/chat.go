package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

type ChatOutbound struct{}

func (o *ChatOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	request.ClearHelpFields()

	// Convert developer role to system role for compatibility
	for i := range request.Messages {
		if request.Messages[i].Role == "developer" {
			request.Messages[i].Role = "system"
		}
	}

	ensureMessageContent(request.Messages)

	if request.Stream != nil && *request.Stream {
		if request.StreamOptions == nil {
			request.StreamOptions = &model.StreamOptions{IncludeUsage: true}
		} else if !request.StreamOptions.IncludeUsage {
			request.StreamOptions.IncludeUsage = true
		}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/chat/completions"
	req.URL = parsedUrl
	req.Method = http.MethodPost
	return req, nil
}

// ensureMessageContent 保证每条消息都带合法的 content 字段。
// 上游（如 opendesign、deepseek 官方）要求 content 必须是 string 或数组；
// 历史会话里模型调用工具但无文本时，或工具结果为空时，本地转换会得到空
// MessageContent，被 MarshalJSON 序列化成 null 或直接省略该字段，导致上游 422。
// 这里在序列化前统一补成 ""（仅作用于 OpenAI 兼容出站，不影响 Anthropic/Gemini 出站）。
func ensureMessageContent(messages []model.Message) {
	for i := range messages {
		m := &messages[i]
		hasContent := (m.Content.Content != nil && *m.Content.Content != "") || len(m.Content.MultipleContent) > 0
		if hasContent {
			continue
		}
		empty := ""
		m.Content = model.MessageContent{Content: &empty}
	}
}

func (o *ChatOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	var resp model.InternalLLMResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &resp, nil
}

func (o *ChatOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	var errCheck struct {
		Error *model.ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(eventData, &errCheck); err == nil && errCheck.Error != nil {
		return nil, &model.ResponseError{
			Detail: *errCheck.Error,
		}
	}

	var resp model.InternalLLMResponse
	if err := json.Unmarshal(eventData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream chunk: %w", err)
	}
	return &resp, nil
}
