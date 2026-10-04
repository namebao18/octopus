package relay

import (
	"fmt"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestWindowFor(t *testing.T) {
	windows := map[string]int{
		"6:Qwen/Qwen3.5-27B": 131072,
		"1:THUDM/GLM-4-9B-0414": 33792,
		"某模型":                  65536,
	}

	cases := []struct {
		chID  int
		model string
		want  int
	}{
		{6, "Qwen/Qwen3.5-27B", 131072},      // 精确「渠道:模型」
		{1, "THUDM/GLM-4-9B-0414", 33792},    // 精确
		{99, "某模型", 65536},                   // 仅模型名兜底
		{99, "未配置的模型", 0},                    // 未知 → 0（不过滤）
		{6, "别的模型", 0},                       // 渠道对但模型不对 → 0
	}
	for _, c := range cases {
		if got := windowFor(windows, c.chID, c.model); got != c.want {
			t.Errorf("windowFor(ch=%d, %q) = %d, 期望 %d", c.chID, c.model, got, c.want)
		}
	}

	if got := windowFor(nil, 6, "Qwen/Qwen3.5-27B"); got != 0 {
		t.Errorf("空配置应返回 0，实得 %d", got)
	}
}

func TestFilterGroupByContextWindow(t *testing.T) {
	// 用一个「按渠道ID返回名字」的桩
	name := func(id int) string { return fmt.Sprintf("ch%d", id) }

	group := model.Group{Items: []model.GroupItem{
		{ChannelID: 6, ModelName: "Qwen/Qwen3.5-27B", Priority: 1},   // 窗口 131072
		{ChannelID: 1, ModelName: "THUDM/GLM-4-9B-0414", Priority: 2}, // 窗口 33792
		{ChannelID: 7, ModelName: "deepseek-v4-flash", Priority: 3},  // 未配置 → 保留
	}}

	t.Run("无配置时不过滤", func(t *testing.T) {
		// parseContextWindows 读的是设置（测试环境未初始化 DB）→ 空 → 原样返回
		got, skipped := filterGroupByContextWindow(group, name, 100000)
		if len(got.Items) != 3 || len(skipped) != 0 {
			t.Errorf("无配置应原样返回，实得 items=%d skipped=%d", len(got.Items), len(skipped))
		}
	})

	t.Run("inputTokens=0 不过滤", func(t *testing.T) {
		got, skipped := filterGroupByContextWindow(group, name, 0)
		if len(got.Items) != 3 || len(skipped) != 0 {
			t.Errorf("inputTokens=0 应原样返回，实得 items=%d", len(got.Items))
		}
	})
}
