package verifier

import (
	"fmt"
	"strings"

	"workbench/internal/analyzer"
	"workbench/internal/model"
)

// 置信度三档分级阈值
const (
	RefuseThreshold         = 0.60 // 低于此值：拒答，转人工
	ConfidenceAutoThreshold = 0.90 // 高于此值：可自动处理建议
)

// Verifier 对 AI 分析结果做补充验证，给出判断依据并校准置信度。
type Verifier struct {
	dataset func() []model.VerifiedCase
}

func New(dataset func() []model.VerifiedCase) *Verifier {
	return &Verifier{dataset: dataset}
}

// Verify 原地校准分析结果，返回验证结论列表。panic 时降级为跳过校准，不拖垮分析。
func (v *Verifier) Verify(content string, r *analyzer.Result) (reasons []string) {
	defer func() {
		if rec := recover(); rec != nil {
			reasons = []string{fmt.Sprintf("验证器异常，已降级跳过校准：%v", rec)}
		}
	}()
	reasons = []string{}

	// 1. 证据一致性校验
	if len(r.Evidence) == 0 {
		r.Confidence -= 0.20
		reasons = append(reasons, "判断依据为空，把握度下调")
	}

	// 2. 历史案例交叉验证
	similar := v.similarCases(content, r.Category)
	if len(similar) > 0 {
		r.Confidence += 0.05
		reasons = append(reasons, fmt.Sprintf("与 %d 条已确认历史案例一致", len(similar)))
	} else if r.Category != "其他" {
		r.Confidence -= 0.05
		reasons = append(reasons, "无同类已确认历史案例，建议谨慎")
	}

	// 3. 拒答判定（三档分级）
	if r.Confidence < RefuseThreshold || r.Category == "其他" {
		r.Refused = true
		r.HumanTakeover = true
		r.TakeoverReason = "置信度过低或类型未覆盖，拒绝自动处理"
		r.RefusalSummary = buildRefusalSummary(r)
		reasons = append(reasons, "拒答："+r.TakeoverReason)
	}

	if r.Confidence < 0 {
		r.Confidence = 0
	}
	if r.Confidence > 1 {
		r.Confidence = 1
	}
	return reasons
}

// buildRefusalSummary 生成拒答时附带的「已尝试摘要」，节省人工复现时间。
func buildRefusalSummary(r *analyzer.Result) string {
	ev := "无"
	if len(r.Evidence) > 0 {
		ev = strings.Join(r.Evidence, "；")
	}
	return fmt.Sprintf("已尝试：分类=%s；置信度 %.0f%%；判断依据：%s。因把握度不足，判定无法可靠自动处理，建议转人工。",
		r.Category, r.Confidence*100, ev)
}

// similarCases 用字符 bigram 的 Jaccard 相似度找同类历史案例。
func (v *Verifier) similarCases(content, category string) []model.VerifiedCase {
	out := []model.VerifiedCase{}
	for _, c := range v.dataset() {
		if c.Category != category {
			continue
		}
		if jaccard(content, c.Content) >= 0.30 {
			out = append(out, c)
		}
	}
	return out
}

func bigrams(s string) map[string]struct{} {
	runes := []rune(s)
	m := make(map[string]struct{}, len(runes))
	for i := 0; i+1 < len(runes); i++ {
		m[string(runes[i:i+2])] = struct{}{}
	}
	return m
}

func jaccard(a, b string) float64 {
	ma, mb := bigrams(a), bigrams(b)
	if len(ma) == 0 || len(mb) == 0 {
		return 0
	}
	inter := 0
	for k := range ma {
		if _, ok := mb[k]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(ma)+len(mb)-inter)
}
