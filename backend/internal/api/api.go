package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"workbench/internal/analyzer"
	"workbench/internal/demo"
	"workbench/internal/engine"
	"workbench/internal/model"
	"workbench/internal/store"
)

type Handler struct {
	store  *store.Store
	engine *engine.Engine
	mock   *analyzer.MockLLM
	demo   *demo.Runner
	log    *slog.Logger
}

func New(s *store.Store, e *engine.Engine, m *analyzer.MockLLM, d *demo.Runner, log *slog.Logger) *Handler {
	return &Handler{store: s, engine: e, mock: m, demo: d, log: log}
}

// Register 将 /api/v1 路由注册到传入的 mux。
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tickets", h.listTickets)
	mux.HandleFunc("POST /api/v1/tickets", h.createTicket)
	mux.HandleFunc("GET /api/v1/tickets/{id}", h.getTicket)
	mux.HandleFunc("DELETE /api/v1/tickets/{id}", h.deleteTicket)
	mux.HandleFunc("PUT /api/v1/tickets/{id}/status", h.updateStatus)
	mux.HandleFunc("POST /api/v1/tickets/{id}/analyze", h.analyze)
	mux.HandleFunc("POST /api/v1/tickets/{id}/analyze/cancel", h.cancelAnalyze)
	mux.HandleFunc("PUT /api/v1/tickets/{id}/analysis/{aid}/confirm", h.confirm)
	mux.HandleFunc("POST /api/v1/tickets/{id}/actions/{aid}", h.runAction)

	mux.HandleFunc("GET /api/v1/queue", h.listQueue)
	mux.HandleFunc("GET /api/v1/queue/pop", h.popQueue)

	mux.HandleFunc("GET /api/v1/actions", h.listActions)
	mux.HandleFunc("POST /api/v1/actions", h.addAction)
	mux.HandleFunc("DELETE /api/v1/actions/{id}", h.deleteAction)

	mux.HandleFunc("GET /api/v1/audit", h.listAudit)

	mux.HandleFunc("GET /api/v1/inbox", h.listInbox)
	mux.HandleFunc("POST /api/v1/inbox/{id}/handle", h.handleMessage)
}

// RegisterDemo 注册演示/测试接口（仅在 -demo 启动时调用）。
func (h *Handler) RegisterDemo(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/mock/preset", h.setMockPreset)
	mux.HandleFunc("POST /api/v1/mock/reset", h.resetMockPreset)
	mux.HandleFunc("POST /api/v1/demo/run", h.runDemo)
	mux.HandleFunc("GET /api/v1/demo/logs", h.demoLogs)
}

/* ---------- 响应辅助 ---------- */

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

/* ---------- 工单 ---------- */

func (h *Handler) listTickets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("include_deleted") == "true" {
		writeJSON(w, http.StatusOK, h.store.ListAllTickets())
		return
	}
	writeJSON(w, http.StatusOK, h.store.ListTickets())
}

type createReq struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func (h *Handler) createTicket(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Title == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, errors.New("title and content required"))
		return
	}
	t, err := h.store.CreateTicket(req.Title, req.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.log.Info("ticket created", "ticket", t.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (h *Handler) getTicket(w http.ResponseWriter, r *http.Request) {
	t, err := h.store.GetTicket(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) deleteTicket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.SoftDelete(id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	h.log.Info("ticket soft-deleted", "ticket", id)
	w.WriteHeader(http.StatusNoContent)
}

type statusReq struct {
	Status string `json:"status"`
}

var validStatuses = map[string]bool{
	model.StatusPending:     true,
	model.StatusNeedsReview: true,
	model.StatusResolved:    true,
	model.StatusNeedsInfo:   true,
	model.StatusCanceled:    true,
}

func (h *Handler) updateStatus(w http.ResponseWriter, r *http.Request) {
	var req statusReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !validStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, errors.New("invalid status"))
		return
	}
	id := r.PathValue("id")
	t, err := h.store.UpdateTicket(id, func(t *model.Ticket) error {
		t.Status = req.Status
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	h.log.Info("ticket status updated", "ticket", t.ID, "status", req.Status)
	writeJSON(w, http.StatusOK, t)
}

/* ---------- 分析 ---------- */

type analyzeReq struct {
	Instruction string `json:"instruction"`
}

func (h *Handler) analyze(w http.ResponseWriter, r *http.Request) {
	var req analyzeReq
	_ = decode(r, &req)
	a, err := h.engine.Submit(r.PathValue("id"), req.Instruction)
	if err != nil {
		if errors.Is(err, engine.ErrEscalated) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a)
}

func (h *Handler) cancelAnalyze(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := h.store.GetTicket(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	// 找最新一条 running 分析打断
	for i := len(t.Analysis) - 1; i >= 0; i-- {
		if t.Analysis[i].Status == model.AnalysisRunning {
			h.engine.Cancel(t.Analysis[i].ID)
			h.log.Info("analysis cancel requested", "ticket", id, "analysis", t.Analysis[i].ID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusConflict, errors.New("no running analysis"))
}

/* ---------- 人工确认 ---------- */

type confirmReq struct {
	Category          string `json:"category"`
	Priority          string `json:"priority"`
	Summary           string `json:"summary"`
	SuggestedAssignee string `json:"suggested_assignee"`
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	var req confirmReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	err := h.store.ConfirmAnalysis(
		r.PathValue("id"),
		r.PathValue("aid"),
		req.Category, req.Priority, req.Summary, req.SuggestedAssignee,
	)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	h.log.Info("analysis confirmed", "ticket", r.PathValue("id"), "analysis", r.PathValue("aid"))
	w.WriteHeader(http.StatusNoContent)
}

/* ---------- 队列 ---------- */

func (h *Handler) listQueue(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.QueueSnapshot())
}

func (h *Handler) popQueue(w http.ResponseWriter, _ *http.Request) {
	id, err := h.store.PopFront()
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	t, err := h.store.GetTicket(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

/* ---------- 预设动作 ---------- */

func (h *Handler) listActions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.ListActions())
}

type actionReq struct {
	Name        string `json:"name"`
	Instruction string `json:"instruction"`
	Shortcut    string `json:"shortcut"`
}

func (h *Handler) addAction(w http.ResponseWriter, r *http.Request) {
	var req actionReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.Instruction == "" {
		writeError(w, http.StatusBadRequest, errors.New("name and instruction required"))
		return
	}
	a, err := h.store.AddAction(req.Name, req.Instruction, req.Shortcut)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	h.log.Info("action added", "action", a.ID, "name", a.Name)
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) deleteAction(w http.ResponseWriter, r *http.Request) {
	if err := h.store.DeleteAction(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runAction 执行预设动作：用预设指令触发一次重分析（重做工单自动插队）。
func (h *Handler) runAction(w http.ResponseWriter, r *http.Request) {
	a, err := h.store.GetAction(r.PathValue("aid"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	_, err = h.engine.Submit(r.PathValue("id"), a.Instruction)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	h.log.Info("action executed", "ticket", r.PathValue("id"), "action", a.Name)
	h.store.AddAudit(model.AuditActionExec, r.PathValue("id"), "执行预设动作："+a.Name+"（"+a.Instruction+"）", "", "")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "submitted", "instruction": a.Instruction})
}

/* ---------- 审计 ---------- */

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	ticketID := r.URL.Query().Get("ticket_id")
	writeJSON(w, http.StatusOK, h.store.ListAudit(ticketID))
}

/* ---------- 收件箱（服务器推送） ---------- */

func (h *Handler) listInbox(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.ListMessages())
}

func (h *Handler) handleMessage(w http.ResponseWriter, r *http.Request) {
	t, err := h.store.HandleMessage(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	h.log.Info("message handled", "message", r.PathValue("id"), "ticket", t.ID)
	writeJSON(w, http.StatusCreated, t)
}

/* ---------- 演示 / mock（仅 -demo 模式） ---------- */

func (h *Handler) setMockPreset(w http.ResponseWriter, r *http.Request) {
	var p analyzer.Preset
	if err := decode(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	h.mock.SetPreset(&p)
	h.log.Info("mock preset set", "delay_ms", p.DelayMS, "confidence", p.Confidence)
	writeJSON(w, http.StatusOK, map[string]string{"status": "preset set"})
}

func (h *Handler) resetMockPreset(w http.ResponseWriter, _ *http.Request) {
	h.mock.ResetPreset()
	writeJSON(w, http.StatusOK, map[string]string{"status": "preset reset"})
}

type demoRunReq struct {
	Scene string `json:"scene"`
}

func (h *Handler) runDemo(w http.ResponseWriter, r *http.Request) {
	var req demoRunReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	h.demo.Run(req.Scene)
	h.store.AddAudit(model.AuditDemo, "", "运行演示场景："+req.Scene, "", "")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started", "scene": req.Scene})
}

func (h *Handler) demoLogs(w http.ResponseWriter, _ *http.Request) {
	logs, running := h.demo.Logs()
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "running": running})
}
