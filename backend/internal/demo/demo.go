package demo

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"workbench/internal/analyzer"
	"workbench/internal/engine"
	"workbench/internal/model"
	"workbench/internal/store"
)

// Runner 演示场景编排器：一个场景一段流程，日志实时可查。
type Runner struct {
	store  *store.Store
	engine *engine.Engine
	mock   *analyzer.MockLLM
	log    *slog.Logger

	mu      sync.Mutex
	logs    []string
	running bool
}

func New(st *store.Store, e *engine.Engine, m *analyzer.MockLLM, logger *slog.Logger) *Runner {
	return &Runner{store: st, engine: e, mock: m, log: logger}
}

// Run 异步运行场景，立即返回。
func (r *Runner) Run(scene string) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.logs = []string{}
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			r.running = false
			r.mu.Unlock()
		}()
		r.runScene(scene)
	}()
}

// Logs 返回当前日志与运行状态。
func (r *Runner) Logs() ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.logs))
	copy(out, r.logs)
	return out, r.running
}

func (r *Runner) logf(format string, args ...any) {
	r.mu.Lock()
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	r.logs = append(r.logs, line)
	r.mu.Unlock()
	r.log.Info("demo", "msg", fmt.Sprintf(format, args...))
}

func (r *Runner) runScene(scene string) {
	switch scene {
	case "normal":
		r.sceneNormal()
	case "wrong":
		r.sceneWrong()
	case "lowconf":
		r.sceneLowConf()
	case "slow":
		r.sceneSlow()
	case "missing":
		r.sceneMissing()
	default:
		r.logf("未知场景：%s", scene)
	}
}

/* ---------- 辅助 ---------- */

func (r *Runner) createTicket(title, content string) string {
	t, err := r.store.CreateTicket(title, content)
	if err != nil {
		r.logf("创建工单失败：%v", err)
		return ""
	}
	r.logf("已创建工单：%s", title)
	return t.ID
}

func (r *Runner) waitStatus(id, want string, timeout time.Duration) *model.Ticket {
	deadline := time.Now().Add(timeout)
	for {
		t, err := r.store.GetTicket(id)
		if err == nil && t.Status == want {
			return t
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (r *Runner) latestDone(t *model.Ticket) *model.Analysis {
	for i := len(t.Analysis) - 1; i >= 0; i-- {
		if t.Analysis[i].Status == model.AnalysisDone {
			return &t.Analysis[i]
		}
	}
	return nil
}

func joinEvidence(ev []string) string {
	if len(ev) == 0 {
		return "无"
	}
	return strings.Join(ev, "；")
}

/* ---------- 场景 ---------- */

func (r *Runner) sceneNormal() {
	r.logf("开始场景：正常分析 + 人工确认")
	r.mock.ResetPreset()
	id := r.createTicket("申请退款", "我买的衣服要退款 800 元，订单号 123456，请尽快处理")
	if id == "" {
		return
	}
	r.engine.Submit(id, "")
	r.logf("已触发 AI 分析…")
	t := r.waitStatus(id, model.StatusNeedsReview, 10*time.Second)
	if t == nil {
		r.logf("等待分析超时")
		return
	}
	a := r.latestDone(t)
	r.logf("AI 分析完成：分类=%s 优先级=%s 置信度=%d%%", a.Category, a.Priority, int(a.Confidence*100))
	r.logf("判断依据：%s", joinEvidence(a.Evidence))
	r.store.ConfirmAnalysis(id, a.ID, a.Category, a.Priority, a.Summary, a.SuggestedAssignee)
	r.logf("模拟人工点击【确认】→ 工单已处理")
	r.logf("场景结束")
}

func (r *Runner) sceneWrong() {
	r.logf("开始场景：AI 给出错误答案，人工修正")
	r.mock.SetPreset(&analyzer.Preset{
		DelayMS:    800,
		Confidence: 0.9,
		Force: &analyzer.ForcedResult{
			Category:          "登录异常",
			Priority:          "中",
			Summary:           "疑似登录问题",
			SuggestedAssignee: "技术运维",
			AutoFixable:       true,
		},
	})
	id := r.createTicket("申请退款", "我买的衣服要退款 800 元，订单号 123456")
	if id == "" {
		r.mock.ResetPreset()
		return
	}
	r.engine.Submit(id, "")
	r.logf("已触发 AI 分析（预置了错误答案）")
	t := r.waitStatus(id, model.StatusNeedsReview, 10*time.Second)
	if t == nil {
		r.mock.ResetPreset()
		r.logf("等待分析超时")
		return
	}
	a := r.latestDone(t)
	r.logf("AI 返回：分类=%s（错误！实际应为退款申请）", a.Category)
	r.store.ConfirmAnalysis(id, a.ID, "退款申请", "高", "用户申请退款 800 元", "退款专员")
	r.logf("模拟人工点击【修改】→ 分类改为「退款申请」，已确认")
	r.mock.ResetPreset()
	r.logf("场景结束")
}

func (r *Runner) sceneLowConf() {
	r.logf("开始场景：低置信度触发人工接管")
	r.mock.SetPreset(&analyzer.Preset{
		DelayMS:    800,
		Confidence: 0.3,
		Force:      &analyzer.ForcedResult{Category: "其他", Priority: "低", Summary: "无法确定类型"},
	})
	id := r.createTicket("遇到点问题", "最近感觉系统不对劲，也说不上来")
	if id == "" {
		r.mock.ResetPreset()
		return
	}
	r.engine.Submit(id, "")
	r.logf("已触发 AI 分析（预置低置信度）")
	t := r.waitStatus(id, model.StatusNeedsReview, 10*time.Second)
	if t == nil {
		r.mock.ResetPreset()
		r.logf("等待分析超时")
		return
	}
	a := r.latestDone(t)
	r.logf("AI 置信度仅 %d%%，验证器判定：建议人工接管", int(a.Confidence*100))
	r.mock.ResetPreset()
	r.logf("场景结束")
}

func (r *Runner) sceneSlow() {
	r.logf("开始场景：慢分析 + 强行打断")
	r.mock.SetPreset(&analyzer.Preset{
		DelayMS:    30000,
		Confidence: 0.8,
		Force:      &analyzer.ForcedResult{Category: "退款申请", Priority: "高", Summary: "退款"},
	})
	id := r.createTicket("申请退款", "要退款 1000 元，订单号 888")
	if id == "" {
		r.mock.ResetPreset()
		return
	}
	a, _ := r.engine.Submit(id, "")
	r.logf("AI 分析耗时约 30s，2 秒后模拟打断…")
	time.Sleep(2 * time.Second)
	r.engine.Cancel(a.ID)
	r.logf("模拟人工点击【打断】")
	t := r.waitStatus(id, model.StatusCanceled, 5*time.Second)
	if t == nil {
		r.mock.ResetPreset()
		r.logf("等待打断状态超时")
		return
	}
	r.logf("打断成功，工单状态=%s", t.Status)
	r.mock.ResetPreset()
	r.logf("场景结束")
}

func (r *Runner) sceneMissing() {
	r.logf("开始场景：信息不足给出补充建议")
	r.mock.ResetPreset()
	id := r.createTicket("要退款", "我要退款，快点处理")
	if id == "" {
		return
	}
	r.engine.Submit(id, "")
	r.logf("已触发 AI 分析…")
	t := r.waitStatus(id, model.StatusNeedsReview, 10*time.Second)
	if t == nil {
		r.logf("等待分析超时")
		return
	}
	a := r.latestDone(t)
	if a.NeedsMoreInfo {
		r.logf("AI 判定信息不足：%s", a.SupplementSuggestion)
	} else {
		r.logf("AI 未判定信息不足（分类=%s）", a.Category)
	}
	r.logf("场景结束")
}
