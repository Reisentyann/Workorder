package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"workbench/internal/analyzer"
	"workbench/internal/model"
	"workbench/internal/sanitize"
	"workbench/internal/store"
	"workbench/internal/verifier"
)

// ErrEscalated 工单已熔断升级，拒绝再次自动分析。
var ErrEscalated = errors.New("ticket escalated, manual handling required")

type job struct {
	ticketID    string
	analysisID  string
	instruction string
}

// 熔断阈值
const (
	maxConsecutiveLowQuality = 3 // 连续低质/拒答次数上限
	maxTotalAnalysis         = 5 // 累计分析次数上限
)

// Engine AI 处理机：单 worker 串行消费，状态稳定可控。
type Engine struct {
	llm      analyzer.LLMClient
	verifier *verifier.Verifier
	store    *store.Store
	log      *slog.Logger
	timeout  time.Duration

	jobs    chan job
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	// pendingCancel 记录在任务尚未开始执行时到达的取消请求，run 启动后立即生效。
	pendingCancel map[string]bool
}

func New(llm analyzer.LLMClient, v *verifier.Verifier, s *store.Store, log *slog.Logger, timeout time.Duration) *Engine {
	return &Engine{
		llm:           llm,
		verifier:      v,
		store:         s,
		log:           log,
		timeout:       timeout,
		jobs:          make(chan job, 100),
		cancels:       map[string]context.CancelFunc{},
		pendingCancel: map[string]bool{},
	}
}

// Start 启动单 worker 协程，串行处理任务。
func (e *Engine) Start() {
	e.recoverStale()
	go e.worker()
}

// recoverStale 启动恢复：清理上次崩溃残留的 analyzing 工单，防止永久卡死。
func (e *Engine) recoverStale() {
	for _, t := range e.store.ListAllTickets() {
		if t.Status != model.StatusAnalyzing {
			continue
		}
		for _, a := range t.Analysis {
			if a.Status == model.AnalysisRunning {
				_ = e.store.SetAnalysisStatus(t.ID, a.ID, model.AnalysisFailed)
			}
		}
		if _, err := e.store.UpdateTicket(t.ID, func(t *model.Ticket) error {
			t.Status = model.StatusPending
			return nil
		}); err != nil {
			e.log.Error("recover stale failed", "ticket", t.ID, "err", err)
			continue
		}
		e.store.AddAudit(model.AuditAnalyze, t.ID, "启动恢复：清理残留分析任务，工单回落待处理", model.StatusAnalyzing, model.StatusPending)
		e.log.Warn("recovered stale ticket", "ticket", t.ID)
	}
}

// Submit 提交分析任务：追加 running 分析记录，工单置为 analyzing，任务入队。
func (e *Engine) Submit(ticketID, instruction string) (*model.Analysis, error) {
	t, err := e.store.GetTicket(ticketID)
	if err != nil {
		return nil, err
	}
	if t.Status == model.StatusEscalated {
		return nil, ErrEscalated
	}
	a := model.Analysis{
		ID:          model.NewID(),
		TicketID:    ticketID,
		Status:      model.AnalysisRunning,
		Instruction: instruction,
		CreatedAt:   time.Now(),
	}
	if err := e.store.AppendAnalysis(ticketID, a); err != nil {
		return nil, err
	}
	if _, err := e.store.UpdateTicket(ticketID, func(t *model.Ticket) error {
		t.Status = model.StatusAnalyzing
		return nil
	}); err != nil {
		return nil, err
	}
	// 带指令的重做工单插到处理队列最前面
	if instruction != "" {
		_ = e.store.EnqueueFront(ticketID)
	}
	detail := "触发 AI 分析"
	if instruction != "" {
		detail = "触发 AI 重做（指令：" + instruction + "）"
	}
	e.store.AddAudit(model.AuditAnalyze, ticketID, detail, "", "")
	e.jobs <- job{ticketID: ticketID, analysisID: a.ID, instruction: instruction}
	e.log.Info("analysis submitted", "ticket", ticketID, "analysis", a.ID)
	return &a, nil
}

// Cancel 打断分析。幂等：任务不存在或已结束时直接返回。
func (e *Engine) Cancel(analysisID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cancel, ok := e.cancels[analysisID]; ok {
		cancel()
		return
	}
	e.pendingCancel[analysisID] = true
}

func (e *Engine) worker() {
	for j := range e.jobs {
		e.run(j)
	}
}

func (e *Engine) run(j job) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	e.mu.Lock()
	e.cancels[j.analysisID] = cancel
	if e.pendingCancel[j.analysisID] {
		cancel()
		delete(e.pendingCancel, j.analysisID)
	}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.cancels, j.analysisID)
		delete(e.pendingCancel, j.analysisID)
		e.mu.Unlock()
	}()

	t, err := e.store.GetTicket(j.ticketID)
	if err != nil {
		e.log.Error("ticket not found", "ticket", j.ticketID, "err", err)
		return
	}
	in := analyzer.Input{
		TicketID:    j.ticketID,
		Title:       sanitize.Mask(t.Title),
		Content:     sanitize.Mask(t.Content),
		Instruction: j.instruction,
	}

	type res struct {
		r   *analyzer.Result
		err error
	}
	ch := make(chan res, 1)
	go func() {
		r, err := e.llm.Analyze(ctx, in)
		ch <- res{r, err}
	}()

	select {
	case out := <-ch:
		if out.err != nil {
			switch {
			case errors.Is(out.err, context.Canceled):
				e.canceled(j)
			case errors.Is(out.err, context.DeadlineExceeded):
				e.timedOut(j)
			default:
				e.fail(j, out.err)
			}
			return
		}
		e.succeed(j, in, out.r)
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			e.timedOut(j)
		} else {
			e.canceled(j)
		}
	}
}

func (e *Engine) succeed(j job, in analyzer.Input, r *analyzer.Result) {
	reasons := e.verifier.Verify(in.Content, r)

	a := toAnalysis(j, in, r)
	a.Status = model.AnalysisDone
	a.Confirmed = false
	a.Verified = false

	if err := e.store.UpdateAnalysis(j.ticketID, &a); err != nil {
		e.log.Error("update analysis result failed", "ticket", j.ticketID, "err", err)
		e.fail(j, err)
		return
	}
	if _, err := e.store.UpdateTicket(j.ticketID, func(t *model.Ticket) error {
		t.Status = model.StatusNeedsReview
		return nil
	}); err != nil {
		e.log.Error("update ticket status failed", "ticket", j.ticketID, "err", err)
		return
	}
	e.log.Info("analysis succeeded",
		"ticket", j.ticketID,
		"category", r.Category,
		"confidence", r.Confidence,
		"verification", reasons,
	)
	e.store.AddAudit(model.AuditAnalyze, j.ticketID,
		fmt.Sprintf("AI 分析完成：分类=%s 优先级=%s 置信度=%d%%", r.Category, r.Priority, int(r.Confidence*100)),
		"", model.StatusNeedsReview)
	e.checkCircuitBreaker(j.ticketID)
}

// checkCircuitBreaker 熔断检查：连续低质或累计次数超限则升级人工。
func (e *Engine) checkCircuitBreaker(ticketID string) {
	t, err := e.store.GetTicket(ticketID)
	if err != nil || t.Status == model.StatusEscalated {
		return
	}
	done := 0
	consecutive := 0
	for i := len(t.Analysis) - 1; i >= 0; i-- {
		a := t.Analysis[i]
		if a.Status != model.AnalysisDone {
			continue
		}
		done++
		if a.Refused || a.Confidence < verifier.RefuseThreshold {
			consecutive++
		} else {
			break
		}
	}

	if consecutive >= maxConsecutiveLowQuality || done >= maxTotalAnalysis {
		_, _ = e.store.UpdateTicket(ticketID, func(t *model.Ticket) error {
			t.Status = model.StatusEscalated
			return nil
		})
		reason := fmt.Sprintf("熔断升级：连续低质 %d 次 / 累计分析 %d 次，转人工组长", consecutive, done)
		e.store.AddAudit(model.AuditEscalate, ticketID, reason, "", model.StatusEscalated)
		e.log.Warn("circuit breaker triggered", "ticket", ticketID, "consecutive", consecutive, "total", done)
	}
}

func (e *Engine) fail(j job, err error) {
	_ = e.store.SetAnalysisStatus(j.ticketID, j.analysisID, model.AnalysisFailed)
	_, _ = e.store.UpdateTicket(j.ticketID, func(t *model.Ticket) error {
		t.Status = model.StatusPending
		return nil
	})
	e.log.Error("analysis failed", "ticket", j.ticketID, "err", err)
	e.store.AddAudit(model.AuditAnalyze, j.ticketID, "AI 分析失败："+err.Error()+"，工单回落待处理", "", model.StatusPending)
}

func (e *Engine) timedOut(j job) {
	_ = e.store.SetAnalysisStatus(j.ticketID, j.analysisID, model.AnalysisTimedOut)
	_, _ = e.store.UpdateTicket(j.ticketID, func(t *model.Ticket) error {
		t.Status = model.StatusPending
		return nil
	})
	e.log.Warn("analysis timed out", "ticket", j.ticketID)
	e.store.AddAudit(model.AuditAnalyze, j.ticketID, "AI 分析超时，工单回落待处理（可重试）", "", model.StatusPending)
}

func (e *Engine) canceled(j job) {
	_ = e.store.SetAnalysisStatus(j.ticketID, j.analysisID, model.AnalysisCanceled)
	_, _ = e.store.UpdateTicket(j.ticketID, func(t *model.Ticket) error {
		t.Status = model.StatusCanceled
		return nil
	})
	e.log.Warn("analysis canceled", "ticket", j.ticketID)
	e.store.AddAudit(model.AuditAnalyze, j.ticketID, "AI 分析被人工打断", "", model.StatusCanceled)
}

func toAnalysis(j job, in analyzer.Input, r *analyzer.Result) model.Analysis {
	return model.Analysis{
		ID:                  j.analysisID,
		TicketID:            j.ticketID,
		Category:            r.Category,
		Priority:            r.Priority,
		Summary:             r.Summary,
		Confidence:          r.Confidence,
		Evidence:            r.Evidence,
		SuggestedAssignee:   r.SuggestedAssignee,
		AutoFixable:         r.AutoFixable,
		AutoFixSuggestion:   r.AutoFixSuggestion,
		NeedsMoreInfo:       r.NeedsMoreInfo,
		SupplementSuggestion: r.SupplementSuggestion,
		HumanTakeover:       r.HumanTakeover,
		TakeoverReason:      r.TakeoverReason,
		Refused:             r.Refused,
		RefusalSummary:      r.RefusalSummary,
		Instruction:         in.Instruction,
		CreatedAt:           time.Now(),
	}
}
