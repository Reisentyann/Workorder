package analyzer

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Input LLM 分析的输入。
type Input struct {
	TicketID    string
	Title       string
	Content     string
	Instruction string // 预设指令（重做等），人工指定方向
}

// Result LLM 分析输出（7 字段 + 自动处理/人工接管分支）。
type Result struct {
	Category             string
	Priority             string
	Summary              string
	Confidence           float64 // LLM 自己判断的把握度
	Evidence             []string
	SuggestedAssignee    string
	AutoFixable          bool
	AutoFixSuggestion    string
	NeedsMoreInfo        bool
	SupplementSuggestion string
	HumanTakeover        bool
	TakeoverReason       string
	Refused              bool
	RefusalSummary       string
}

// LLMClient 抽象 LLM，未来可替换真实实现而不改上层（开闭原则）。
type LLMClient interface {
	Analyze(ctx context.Context, in Input) (*Result, error)
}

// ForcedResult 预置的强制答案（演示注入，可故意写错）。
type ForcedResult struct {
	Category          string   `json:"category"`
	Priority          string   `json:"priority"`
	Summary           string   `json:"summary"`
	SuggestedAssignee string   `json:"suggested_assignee"`
	AutoFixable       bool     `json:"auto_fixable"`
	AutoFixSuggestion string   `json:"auto_fix_suggestion,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
}

// Preset 演示场景桩：预置处理速度、置信度与（可选的）强制答案。
type Preset struct {
	DelayMS    int64         `json:"delay_ms"`
	Confidence float64       `json:"confidence"`
	Force      *ForcedResult `json:"force,omitempty"`
}

// MockLLM 用规则 + 策略模式模拟 LLM 分析，无需真实模型。
type MockLLM struct {
	factory *Factory

	mu     sync.Mutex
	preset *Preset
}

func NewMockLLM() *MockLLM {
	return &MockLLM{factory: NewFactory()}
}

// SetPreset 设置演示场景桩，后续分析按预设表现。
func (m *MockLLM) SetPreset(p *Preset) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preset = p
}

// ResetPreset 清除演示场景桩，恢复默认规则引擎。
func (m *MockLLM) ResetPreset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preset = nil
}

func (m *MockLLM) CurrentPreset() *Preset {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.preset == nil {
		return nil
	}
	cp := *m.preset
	return &cp
}

// Analyze 模拟 LLM：若设定了演示场景桩则按桩表现，否则路由到策略。
func (m *MockLLM) Analyze(ctx context.Context, in Input) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	preset := m.CurrentPreset()
	delay := 1500 * time.Millisecond
	if preset != nil && preset.DelayMS > 0 {
		delay = time.Duration(preset.DelayMS) * time.Millisecond
	}

	// 模拟 LLM 思考耗时，期间响应取消/超时（便于演示打断）。
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if preset != nil && preset.Force != nil {
		return applyForce(in, preset), nil
	}

	text := in.Title + " " + in.Content
	s, hits := m.factory.Route(text, in.Instruction)

	r := &Result{
		Category:          s.Type(),
		Priority:          s.Priority(in.Content),
		Summary:           s.Summary(in.Title, in.Content),
		SuggestedAssignee: s.Assignee(),
		AutoFixable:       s.AutoFixable(),
		AutoFixSuggestion: s.AutoFixSuggestion(in.Content),
	}

	// 判断依据 / 证据
	evidence := []string{}
	if in.Instruction != "" {
		evidence = append(evidence, "人工指令: "+in.Instruction)
	}
	for _, k := range hits {
		evidence = append(evidence, "关键词命中: "+k)
	}
	if s.Type() == "其他" {
		evidence = append(evidence, "未命中已知类型，路由到兜底策略")
	}
	r.Evidence = evidence

	// 置信度：LLM 自己判断（模拟）。人工指令 > 强命中 > 兜底。
	r.Confidence = m.judgeConfidence(in, s, hits)

	// 信息完整性初判：缺关键字段则标记需补充信息
	missing := []string{}
	matchers := s.RequiredFieldMatchers()
	for i, f := range s.RequiredFields() {
		hit := false
		if i < len(matchers) {
			for _, m := range matchers[i] {
				if strings.Contains(in.Content, m) {
					hit = true
					break
				}
			}
		} else if strings.Contains(in.Content, f) {
			hit = true
		}
		if !hit {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		r.NeedsMoreInfo = true
		r.SupplementSuggestion = "需补充信息：" + strings.Join(missing, "、")
		r.Confidence -= 0.10 * float64(len(missing))
		if r.Confidence < 0.05 {
			r.Confidence = 0.05
		}
		evidence = append(evidence, "信息不完整，缺少: "+strings.Join(missing, "、"))
		r.Evidence = evidence
	}

	return r, nil
}

// judgeConfidence 模拟 LLM 对把握度的判断。
func (m *MockLLM) judgeConfidence(in Input, s Strategy, hits []string) float64 {
	if in.Instruction != "" {
		return 0.95
	}
	if s.Type() == "其他" {
		return 0.30
	}
	c := 0.60 + 0.08*float64(len(hits))
	if c > 0.90 {
		c = 0.90
	}
	return c
}

// Factory 工厂模式 + 策略注册表。新增类型只需 Register，不改已有代码。
type Factory struct {
	strategies map[string]Strategy
	order      []Strategy
}

func NewFactory() *Factory {
	f := &Factory{strategies: map[string]Strategy{}}
	f.Register(&RefundStrategy{})
	f.Register(&LoginStrategy{})
	f.Register(&InvoiceStrategy{})
	f.Register(&LogisticsStrategy{})
	f.Register(&DefaultStrategy{})
	return f
}

// Register 注册策略（开闭原则：对扩展开放）。
func (f *Factory) Register(s Strategy) {
	f.strategies[s.Type()] = s
	f.order = append(f.order, s)
}

// Route 根据文本关键词 + 人工指令路由到策略，返回命中的关键词。
func (f *Factory) Route(text, instruction string) (Strategy, []string) {
	// 人工指令优先：强制路由到指定类型
	if instruction != "" {
		if s, ok := f.matchByInstruction(instruction); ok {
			return s, []string{instruction}
		}
	}

	best := Strategy(nil)
	bestHits := []string{}
	bestScore := 0
	for _, s := range f.order {
		if s.Type() == "其他" {
			continue
		}
		hits := matchKeywords(text, s.Keywords())
		if len(hits) > bestScore {
			best = s
			bestHits = hits
			bestScore = len(hits)
		}
	}
	if best != nil {
		return best, bestHits
	}
	return f.strategies["其他"], nil
}

// matchByInstruction 从指令里找类型名，支持"重做/分类改成退款"之类。
func (f *Factory) matchByInstruction(instruction string) (Strategy, bool) {
	for _, s := range f.order {
		if s.Type() == "其他" {
			continue
		}
		if strings.Contains(instruction, s.Type()) {
			return s, true
		}
		// 也匹配类型名的核心词，如"退款"
		for _, k := range s.Keywords() {
			if len(k) >= 2 && strings.Contains(instruction, k) {
				return s, true
			}
		}
	}
	return nil, false
}

func matchKeywords(text string, keywords []string) []string {
	hits := []string{}
	for _, k := range keywords {
		if strings.Contains(text, k) {
			hits = append(hits, k)
		}
	}
	return hits
}

func fmtSummary(title string) string {
	if title == "" {
		return "（无标题）"
	}
	return title
}

// applyForce 用演示场景桩的强制答案构造结果。
func applyForce(in Input, p *Preset) *Result {
	f := p.Force
	r := &Result{
		Category:          f.Category,
		Priority:          f.Priority,
		Summary:           f.Summary,
		Confidence:        p.Confidence,
		Evidence:          f.Evidence,
		SuggestedAssignee: f.SuggestedAssignee,
		AutoFixable:       f.AutoFixable,
		AutoFixSuggestion: f.AutoFixSuggestion,
	}
	if len(r.Evidence) == 0 {
		r.Evidence = []string{"演示注入答案"}
	}
	return r
}
