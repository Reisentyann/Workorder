package verifier

import (
	"testing"

	"workbench/internal/analyzer"
	"workbench/internal/model"
)

func TestLowConfidenceTriggersTakeover(t *testing.T) {
	v := New(func() []model.VerifiedCase { return nil })
	r := &analyzer.Result{Category: "其他", Confidence: 0.3, Evidence: nil}
	v.Verify("今天天气不错", r)
	if !r.HumanTakeover {
		t.Error("low confidence should trigger human takeover")
	}
	if !r.Refused {
		t.Error("low confidence should trigger refuse-to-answer")
	}
	if r.RefusalSummary == "" {
		t.Error("refusal summary should not be empty")
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		t.Errorf("confidence out of range: %v", r.Confidence)
	}
}

func TestSimilarCaseBoostsConfidence(t *testing.T) {
	dataset := []model.VerifiedCase{
		{Category: "退款申请", Content: "我买了件衣服要退款，订单号123，金额800元"},
	}
	v := New(func() []model.VerifiedCase { return dataset })
	r := &analyzer.Result{Category: "退款申请", Confidence: 0.7, Evidence: []string{"退款"}}
	reasons := v.Verify("我买了件衣服要退款，订单号456，金额500元", r)
	if r.Confidence <= 0.7 {
		t.Errorf("confidence = %v, want boost from similar case", r.Confidence)
	}
	if len(reasons) == 0 {
		t.Error("expected verification reasons")
	}
}

func TestJaccard(t *testing.T) {
	if jaccard("退款申请", "退款申请") <= 0 {
		t.Error("identical strings should have high similarity")
	}
	if jaccard("退款", "abc") != 0 {
		t.Error("disjoint strings should have zero similarity")
	}
}
