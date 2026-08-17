package engine

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"workbench/internal/analyzer"
	"workbench/internal/model"
	"workbench/internal/store"
	"workbench/internal/verifier"
)

func newEngine(t *testing.T) (*Engine, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	llm := analyzer.NewMockLLM()
	v := verifier.New(st.Dataset)
	eng := New(llm, v, st, logger, 5*time.Second)
	eng.Start()
	return eng, st
}

func waitFor(t *testing.T, st *store.Store, id, status string) *model.Ticket {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := st.GetTicket(id)
		if err == nil && got.Status == status {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for status %q", status)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestEngineFullFlow(t *testing.T) {
	eng, st := newEngine(t)
	tk, _ := st.CreateTicket("登录异常", "账号登不上，验证码也收不到")

	a, err := eng.Submit(tk.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != model.AnalysisRunning {
		t.Errorf("analysis status = %q, want running", a.Status)
	}

	got := waitFor(t, st, tk.ID, model.StatusNeedsReview)
	latest := got.Analysis[len(got.Analysis)-1]
	if latest.Status != model.AnalysisDone {
		t.Errorf("analysis status = %q, want done", latest.Status)
	}
	if latest.Category != "登录异常" {
		t.Errorf("category = %q, want 登录异常", latest.Category)
	}
	if !latest.AutoFixable {
		t.Error("login should be auto-fixable")
	}
}

func TestEngineCancel(t *testing.T) {
	eng, st := newEngine(t)
	tk, _ := st.CreateTicket("退款", "要退款 800 元")

	a, err := eng.Submit(tk.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	// 立即打断（可能在任务开始前到达）
	eng.Cancel(a.ID)

	got := waitFor(t, st, tk.ID, model.StatusCanceled)
	if got.Status != model.StatusCanceled {
		t.Errorf("status = %q, want canceled", got.Status)
	}
}

func TestEngineInstructionEnqueuesFront(t *testing.T) {
	eng, st := newEngine(t)
	a, _ := st.CreateTicket("a", "1")
	b, _ := st.CreateTicket("b", "2")

	if _, err := eng.Submit(a.ID, "重做分类退款"); err != nil {
		t.Fatal(err)
	}
	id, err := st.PopFront()
	if err != nil {
		t.Fatal(err)
	}
	if id != a.ID {
		t.Errorf("front = %q, want %q (重做插队)", id, a.ID)
	}
	_ = b
}

func TestEngineTimeout(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	llm := analyzer.NewMockLLM() // 默认 1.5s 延迟
	v := verifier.New(st.Dataset)
	eng := New(llm, v, st, logger, 200*time.Millisecond) // 短超时
	eng.Start()

	tk, _ := st.CreateTicket("退款", "要退款 800 元")
	if _, err := eng.Submit(tk.ID, ""); err != nil {
		t.Fatal(err)
	}
	got := waitFor(t, st, tk.ID, model.StatusPending)
	latest := got.Analysis[len(got.Analysis)-1]
	if latest.Status != model.AnalysisTimedOut {
		t.Errorf("analysis status = %q, want timed_out", latest.Status)
	}
}

func TestEngineRecoverStale(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	llm := analyzer.NewMockLLM()
	v := verifier.New(st.Dataset)

	// 制造崩溃现场：engine 不启动 worker，直接 Submit 留下 analyzing 残留
	eng := New(llm, v, st, logger, 5*time.Second)
	tk, _ := st.CreateTicket("退款", "要退款")
	if _, err := eng.Submit(tk.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetTicket(tk.ID); got.Status != model.StatusAnalyzing {
		t.Fatalf("status = %q, want analyzing", got.Status)
	}

	// 模拟重启：新 engine 启动时应恢复残留
	eng2 := New(llm, v, st, logger, 5*time.Second)
	eng2.Start()
	got, _ := st.GetTicket(tk.ID)
	if got.Status != model.StatusPending {
		t.Errorf("status = %q, want pending (recovered)", got.Status)
	}
	latest := got.Analysis[len(got.Analysis)-1]
	if latest.Status != model.AnalysisFailed {
		t.Errorf("analysis status = %q, want failed (recovered)", latest.Status)
	}
}
