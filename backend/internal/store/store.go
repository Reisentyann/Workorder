package store

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"workbench/internal/model"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrDeleted     = errors.New("ticket deleted")
	ErrQueueEmpty  = errors.New("queue empty")
	ErrActionExists = errors.New("action already exists")
)

// VerifiedCase 已移至 model 包，此处保留类型别名兼容内部引用。
type VerifiedCase = model.VerifiedCase

type dataFile struct {
	Tickets  []model.Ticket  `json:"tickets"`
	Actions  []model.Action  `json:"actions"`
	Queue    []string        `json:"queue"`
	Dataset  []VerifiedCase  `json:"dataset"`
	Messages []model.Message `json:"messages"`
}

type Store struct {
	mu        sync.Mutex
	path      string
	auditPath string
	data      dataFile
	audit     []model.AuditEntry
}

// New 初始化存储，从 JSON 文件加载（不存在则新建）。
func New(dataDir string) (*Store, error) {
	s := &Store{
		path:      filepath.Join(dataDir, "data.json"),
		auditPath: filepath.Join(dataDir, "audit.json"),
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(s.path); err == nil {
		if len(b) > 0 {
			if err := json.Unmarshal(b, &s.data); err != nil {
				return nil, err
			}
		}
	}
	if b, err := os.ReadFile(s.auditPath); err == nil {
		if len(b) > 0 {
			if err := json.Unmarshal(b, &s.audit); err != nil {
				return nil, err
			}
		}
	}
	if s.data.Tickets == nil {
		s.data.Tickets = []model.Ticket{}
	}
	if s.data.Actions == nil {
		s.data.Actions = []model.Action{}
	}
	if s.data.Queue == nil {
		s.data.Queue = []string{}
	}
	if s.data.Dataset == nil {
		s.data.Dataset = []VerifiedCase{}
	}
	if s.audit == nil {
		s.audit = []model.AuditEntry{}
	}
	if s.data.Messages == nil {
		s.data.Messages = []model.Message{}
	}
	// 首次启动：预置几条「服务器推送」的用户反馈消息
	if len(s.data.Messages) == 0 {
		s.data.Messages = seedMessages()
		_ = s.save()
	}
	return s, nil
}

// seedMessages 预置模拟推送消息。
func seedMessages() []model.Message {
	contents := []string{
		"用户反馈：我买的衣服要退款 800 元，订单号 123456，麻烦尽快处理。",
		"用户反馈：账号一直登不上去，验证码也收不到，很着急。",
		"用户反馈：发票抬头写错了，需要重开，税号 91110000XXXX。",
		"用户反馈：快递三天没更新物流了，是不是丢件了，运单号 SF123456789。",
		"用户反馈：我要退款，快点处理。",
	}
	msgs := make([]model.Message, 0, len(contents))
	for _, c := range contents {
		msgs = append(msgs, model.Message{
			ID:        model.NewID(),
			Content:   c,
			CreatedAt: time.Now(),
		})
	}
	return msgs
}

// save 原子写：先写临时文件再 rename，防止中途崩溃写坏。
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		slog.Error("store write failed", "path", s.path, "err", err)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		slog.Error("store rename failed", "path", s.path, "err", err)
		return err
	}
	return nil
}

func (s *Store) lock()   { s.mu.Lock() }
func (s *Store) unlock() { s.mu.Unlock() }

/* ---------- 工单 ---------- */

func (s *Store) CreateTicket(title, content string) (*model.Ticket, error) {
	s.lock()
	defer s.unlock()
	now := time.Now()
	t := model.Ticket{
		ID:        model.NewID(),
		Title:     title,
		Content:   content,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
		Analysis:  []model.Analysis{},
	}
	s.data.Tickets = append(s.data.Tickets, t)
	s.data.Queue = append(s.data.Queue, t.ID)
	if err := s.save(); err != nil {
		return nil, err
	}
	s.addAuditLocked(model.AuditCreate, t.ID, "创建工单："+t.Title, "", model.StatusPending)
	cp := t
	return &cp, nil
}

func (s *Store) findTicket(id string) (*model.Ticket, int) {
	for i := range s.data.Tickets {
		if s.data.Tickets[i].ID == id {
			return &s.data.Tickets[i], i
		}
	}
	return nil, -1
}

func (s *Store) GetTicket(id string) (*model.Ticket, error) {
	s.lock()
	defer s.unlock()
	t, _ := s.findTicket(id)
	if t == nil {
		return nil, ErrNotFound
	}
	if t.Deleted {
		return nil, ErrDeleted
	}
	cp := *t
	return &cp, nil
}

func (s *Store) ListTickets() []model.Ticket {
	s.lock()
	defer s.unlock()
	out := make([]model.Ticket, 0, len(s.data.Tickets))
	for _, t := range s.data.Tickets {
		if !t.Deleted {
			out = append(out, t)
		}
	}
	return out
}

// ListAllTickets 返回全部工单（含软删除），供历史浏览/追责。
func (s *Store) ListAllTickets() []model.Ticket {
	s.lock()
	defer s.unlock()
	out := make([]model.Ticket, len(s.data.Tickets))
	copy(out, s.data.Tickets)
	return out
}

// UpdateTicket 在锁内原地更新，不重建对象（懒标记思想）。
func (s *Store) UpdateTicket(id string, fn func(*model.Ticket) error) (*model.Ticket, error) {
	s.lock()
	defer s.unlock()
	t, _ := s.findTicket(id)
	if t == nil {
		return nil, ErrNotFound
	}
	if t.Deleted {
		return nil, ErrDeleted
	}
	beforeStatus := t.Status
	if err := fn(t); err != nil {
		return nil, err
	}
	t.UpdatedAt = time.Now()
	if err := s.save(); err != nil {
		return nil, err
	}
	if t.Status != beforeStatus {
		s.addAuditLocked(model.AuditStatusChange, id, "更新状态", beforeStatus, t.Status)
	}
	cp := *t
	return &cp, nil
}

// SoftDelete 懒标记软删除，不物理删除重建。
func (s *Store) SoftDelete(id string) error {
	s.lock()
	defer s.unlock()
	t, _ := s.findTicket(id)
	if t == nil {
		return ErrNotFound
	}
	now := time.Now()
	before := t.Status
	t.Deleted = true
	t.DeletedAt = &now
	t.UpdatedAt = now
	s.removeFromQueue(id)
	if err := s.save(); err != nil {
		return err
	}
	s.addAuditLocked(model.AuditDelete, id, "删除工单（懒标记软删除）", before, "deleted")
	return nil
}

/* ---------- 队列（支持重做插队 PushFront） ---------- */

func (s *Store) Enqueue(id string) error {
	s.lock()
	defer s.unlock()
	for _, q := range s.data.Queue {
		if q == id {
			return nil
		}
	}
	s.data.Queue = append(s.data.Queue, id)
	return s.save()
}

// EnqueueFront 让 LLM 重做的工单插到队首。
func (s *Store) EnqueueFront(id string) error {
	s.lock()
	defer s.unlock()
	s.removeFromQueue(id)
	s.data.Queue = append([]string{id}, s.data.Queue...)
	return s.save()
}

func (s *Store) removeFromQueue(id string) {
	out := s.data.Queue[:0]
	for _, q := range s.data.Queue {
		if q != id {
			out = append(out, q)
		}
	}
	s.data.Queue = out
}

// PopFront 取出队首工单给人工审查。
func (s *Store) PopFront() (string, error) {
	s.lock()
	defer s.unlock()
	if len(s.data.Queue) == 0 {
		return "", ErrQueueEmpty
	}
	id := s.data.Queue[0]
	s.data.Queue = s.data.Queue[1:]
	if err := s.save(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) QueueSnapshot() []string {
	s.lock()
	defer s.unlock()
	out := make([]string, len(s.data.Queue))
	copy(out, s.data.Queue)
	return out
}

/* ---------- 分析结果 ---------- */

// AppendAnalysis 追加一条分析结果（版本化，不覆盖历史）。
func (s *Store) AppendAnalysis(ticketID string, a model.Analysis) error {
	s.lock()
	defer s.unlock()
	t, _ := s.findTicket(ticketID)
	if t == nil {
		return ErrNotFound
	}
	t.Analysis = append(t.Analysis, a)
	return s.save()
}

// UpdateAnalysis 覆盖指定分析记录（分析完成后写入结果）。
func (s *Store) UpdateAnalysis(ticketID string, a *model.Analysis) error {
	s.lock()
	defer s.unlock()
	t, _ := s.findTicket(ticketID)
	if t == nil {
		return ErrNotFound
	}
	for i := range t.Analysis {
		if t.Analysis[i].ID == a.ID {
			a.TicketID = ticketID
			t.Analysis[i] = *a
			return s.save()
		}
	}
	return ErrNotFound
}

// SetAnalysisRunning 标记某条分析为进行中。
func (s *Store) SetAnalysisStatus(ticketID, analysisID, status string) error {	s.lock()
	defer s.unlock()
	t, _ := s.findTicket(ticketID)
	if t == nil {
		return ErrNotFound
	}
	for i := range t.Analysis {
		if t.Analysis[i].ID == analysisID {
			t.Analysis[i].Status = status
			return s.save()
		}
	}
	return ErrNotFound
}

/* ---------- 人工确认（验证后计入数据集） ---------- */

// ConfirmAnalysis 人工确认/修改 AI 建议，并把已验证案例计入数据集。
func (s *Store) ConfirmAnalysis(ticketID, analysisID, category, priority, summary, assignee string) error {
	s.lock()
	defer s.unlock()
	t, _ := s.findTicket(ticketID)
	if t == nil {
		return ErrNotFound
	}
	found := false
	for i := range t.Analysis {
		a := &t.Analysis[i]
		if a.ID == analysisID {
			a.Category = category
			a.Priority = priority
			a.Summary = summary
			a.SuggestedAssignee = assignee
			a.Confirmed = true
			a.Verified = true
			found = true
		}
	}
	if !found {
		return ErrNotFound
	}
	t.Category = category
	t.Priority = priority
	t.Summary = summary
	t.Assignee = assignee
	t.Status = model.StatusResolved

	// 计入数据集：取最新一条已确认的分析
	if len(t.Analysis) > 0 {
		latest := t.Analysis[len(t.Analysis)-1]
		s.data.Dataset = append(s.data.Dataset, VerifiedCase{
			Category:  latest.Category,
			Priority:  latest.Priority,
			Content:   t.Content,
			CreatedAt: time.Now(),
		})
	}
	if err := s.save(); err != nil {
		return err
	}
	s.addAuditLocked(model.AuditConfirm, ticketID, "人工确认/修改 AI 建议：分类="+category+" 优先级="+priority, "", model.StatusResolved)
	return nil
}

/* ---------- 数据集 ---------- */

func (s *Store) Dataset() []VerifiedCase {
	s.lock()
	defer s.unlock()
	out := make([]VerifiedCase, len(s.data.Dataset))
	copy(out, s.data.Dataset)
	return out
}

/* ---------- 预设动作 ---------- */

func (s *Store) ListActions() []model.Action {
	s.lock()
	defer s.unlock()
	out := make([]model.Action, len(s.data.Actions))
	copy(out, s.data.Actions)
	return out
}

func (s *Store) GetAction(id string) (*model.Action, error) {
	s.lock()
	defer s.unlock()
	for i := range s.data.Actions {
		if s.data.Actions[i].ID == id {
			cp := s.data.Actions[i]
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) AddAction(name, instruction, shortcut string) (*model.Action, error) {
	s.lock()
	defer s.unlock()
	if shortcut != "" {
		for _, a := range s.data.Actions {
			if a.Shortcut == shortcut {
				return nil, ErrActionExists
			}
		}
	}
	a := model.Action{
		ID:          model.NewID(),
		Name:        name,
		Instruction: instruction,
		Shortcut:    shortcut,
		CreatedAt:   time.Now(),
	}
	s.data.Actions = append(s.data.Actions, a)
	if err := s.save(); err != nil {
		return nil, err
	}
	s.addAuditLocked(model.AuditActionAdd, "", "新增预设动作："+a.Name+"（"+a.Instruction+"）", "", "")
	cp := a
	return &cp, nil
}

func (s *Store) DeleteAction(id string) error {
	s.lock()
	defer s.unlock()
	for i := range s.data.Actions {
		if s.data.Actions[i].ID == id {
			name := s.data.Actions[i].Name
			s.data.Actions = append(s.data.Actions[:i], s.data.Actions[i+1:]...)
			if err := s.save(); err != nil {
				return err
			}
			s.addAuditLocked(model.AuditActionDelete, "", "删除预设动作："+name, "", "")
			return nil
		}
	}
	return ErrNotFound
}

/* ---------- 审计日志（独立 audit.json） ---------- */

// AddAudit 追加一条审计日志，独立原子写到 audit.json。
func (s *Store) AddAudit(action, ticketID, detail, before, after string) {
	s.lock()
	defer s.unlock()
	s.addAuditLocked(action, ticketID, detail, before, after)
}

// addAuditLocked 在已持锁时追加审计（调用方负责加锁）。
func (s *Store) addAuditLocked(action, ticketID, detail, before, after string) {
	e := model.AuditEntry{
		ID:       model.NewID(),
		Time:     time.Now(),
		Action:   action,
		TicketID: ticketID,
		Detail:   detail,
		Before:   before,
		After:    after,
	}
	s.audit = append(s.audit, e)
	_ = s.saveAudit()
}

func (s *Store) saveAudit() error {
	b, err := json.MarshalIndent(s.audit, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.auditPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		slog.Error("audit write failed", "path", s.auditPath, "err", err)
		return err
	}
	if err := os.Rename(tmp, s.auditPath); err != nil {
		slog.Error("audit rename failed", "path", s.auditPath, "err", err)
		return err
	}
	return nil
}

// ListAudit 返回审计日志；ticketID 为空时返回全部。
func (s *Store) ListAudit(ticketID string) []model.AuditEntry {
	s.lock()
	defer s.unlock()
	out := []model.AuditEntry{}
	for _, e := range s.audit {
		if ticketID == "" || e.TicketID == ticketID {
			out = append(out, e)
		}
	}
	return out
}

/* ---------- 收件箱（服务器推送消息） ---------- */

// ListMessages 返回全部推送消息。
func (s *Store) ListMessages() []model.Message {
	s.lock()
	defer s.unlock()
	out := make([]model.Message, len(s.data.Messages))
	copy(out, s.data.Messages)
	return out
}

// HandleMessage 处理一条推送消息：用其内容创建工单并标记已处理。
func (s *Store) HandleMessage(msgID string) (*model.Ticket, error) {
	s.lock()
	defer s.unlock()
	idx := -1
	for i := range s.data.Messages {
		if s.data.Messages[i].ID == msgID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, ErrNotFound
	}
	msg := &s.data.Messages[idx]
	if msg.Handled {
		return nil, errors.New("message already handled")
	}
	now := time.Now()
	title := truncateRunes(msg.Content, 20)
	t := model.Ticket{
		ID:        model.NewID(),
		Title:     title,
		Content:   msg.Content,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
		Analysis:  []model.Analysis{},
	}
	s.data.Tickets = append(s.data.Tickets, t)
	s.data.Queue = append(s.data.Queue, t.ID)
	msg.Handled = true
	msg.TicketID = t.ID
	if err := s.save(); err != nil {
		return nil, err
	}
	s.addAuditLocked(model.AuditCreate, t.ID, "从推送消息创建工单："+title, "", model.StatusPending)
	cp := t
	return &cp, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
