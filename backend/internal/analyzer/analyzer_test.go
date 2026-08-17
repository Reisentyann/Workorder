package analyzer

import (
	"context"
	"testing"
)

func TestRefundRoutingAndAmountPriority(t *testing.T) {
	m := NewMockLLM()
	r, err := m.Analyze(context.Background(), Input{
		Title:   "申请退款",
		Content: "我买了件衣服，退款 800 元，麻烦尽快处理。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Category != "退款申请" {
		t.Errorf("category = %q, want 退款申请", r.Category)
	}
	if r.Priority != "高" {
		t.Errorf("priority = %q, want 高 (金额敏感)", r.Priority)
	}
	if r.SuggestedAssignee != "退款专员" {
		t.Errorf("assignee = %q, want 退款专员", r.SuggestedAssignee)
	}
}

func TestInstructionOverride(t *testing.T) {
	m := NewMockLLM()
	r, err := m.Analyze(context.Background(), Input{
		Title:       "登录不了",
		Content:     "账号登不上去",
		Instruction: "重新分析，把分类改成退款申请",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Category != "退款申请" {
		t.Errorf("category = %q, want 退款申请 (指令强制)", r.Category)
	}
	if r.Confidence < 0.7 {
		t.Errorf("confidence = %v, want 高把握 (人工指令)", r.Confidence)
	}
}

func TestLoginAutoFixable(t *testing.T) {
	m := NewMockLLM()
	r, err := m.Analyze(context.Background(), Input{
		Title:   "登录异常",
		Content: "账号密码都对但就是登不上，验证码也收不到",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Category != "登录异常" {
		t.Errorf("category = %q, want 登录异常", r.Category)
	}
	if !r.AutoFixable {
		t.Error("login should be auto-fixable")
	}
	if r.AutoFixSuggestion == "" {
		t.Error("auto-fix suggestion should not be empty")
	}
}

func TestDefaultRoutingLowConfidence(t *testing.T) {
	m := NewMockLLM()
	r, err := m.Analyze(context.Background(), Input{
		Title:   "随便问问",
		Content: "今天天气不错",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Category != "其他" {
		t.Errorf("category = %q, want 其他", r.Category)
	}
	if r.Confidence >= 0.5 {
		t.Errorf("confidence = %v, want low confidence for unknown", r.Confidence)
	}
}

func TestMissingInfoMarksNeedsMoreInfo(t *testing.T) {
	m := NewMockLLM()
	r, err := m.Analyze(context.Background(), Input{
		Title:   "要退款",
		Content: "我要退款，快点",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.NeedsMoreInfo {
		t.Error("missing 订单号/金额 should mark needs_more_info")
	}
	if r.SupplementSuggestion == "" {
		t.Error("supplement suggestion should not be empty")
	}
}
