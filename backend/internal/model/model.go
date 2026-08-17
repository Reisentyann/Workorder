package model

import (
	"crypto/rand"
	"fmt"
	"time"
)

// NewID 生成 UUID v4（无外部依赖）
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// 工单状态（业务层）
const (
	StatusPending     = "pending"      // 待处理
	StatusAnalyzing   = "analyzing"    // 分析中
	StatusNeedsReview = "needs_review" // 待确认
	StatusResolved    = "resolved"     // 已处理
	StatusNeedsInfo   = "needs_info"   // 需补充信息
	StatusCanceled    = "canceled"     // 已取消
	StatusEscalated   = "escalated"    // 已升级人工（熔断）
)

// 分析结果状态（结果层）
const (
	AnalysisRunning  = "running"
	AnalysisDone     = "done"
	AnalysisCanceled = "canceled"
	AnalysisFailed   = "failed"
	AnalysisTimedOut = "timed_out"
)

// AI 处理机任务状态（执行层）
const (
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskCanceled  = "canceled"
	TaskFailed    = "failed"
	TaskTimedOut  = "timed_out"
)

// 审计动作
const (
	AuditCreate       = "create"
	AuditAnalyze      = "analyze"
	AuditConfirm      = "confirm"
	AuditStatusChange = "status_change"
	AuditDelete       = "delete"
	AuditActionExec   = "action_execute"
	AuditActionAdd    = "action_add"
	AuditActionDelete = "action_delete"
	AuditDemo         = "demo"
	AuditEscalate     = "escalate"
)

// AuditEntry 审计日志条目（用于追责）。
type AuditEntry struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Action   string    `json:"action"`
	TicketID string    `json:"ticket_id,omitempty"`
	Detail   string    `json:"detail"`
	Before   string    `json:"before,omitempty"`
	After    string    `json:"after,omitempty"`
}

type Ticket struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Status    string     `json:"status"`
	Category  string     `json:"category,omitempty"`
	Priority  string     `json:"priority,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Assignee  string     `json:"assignee,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Deleted   bool       `json:"deleted"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Analysis  []Analysis `json:"analysis"`
}

type Analysis struct {
	ID                  string    `json:"id"`
	TicketID            string    `json:"ticket_id"`
	Category            string    `json:"category"`
	Priority            string    `json:"priority"`
	Summary             string    `json:"summary"`
	Confidence          float64   `json:"confidence"`
	Evidence            []string  `json:"evidence"`
	SuggestedAssignee   string    `json:"suggested_assignee"`
	AutoFixable         bool      `json:"auto_fixable"`
	AutoFixSuggestion   string    `json:"auto_fix_suggestion,omitempty"`
	NeedsMoreInfo       bool      `json:"needs_more_info"`
	SupplementSuggestion string   `json:"supplement_suggestion,omitempty"`
	HumanTakeover       bool      `json:"human_takeover"`
	TakeoverReason      string    `json:"takeover_reason,omitempty"`
	Status              string    `json:"status"`
	Instruction         string    `json:"instruction,omitempty"`
	Refused             bool      `json:"refused"`
	RefusalSummary      string    `json:"refusal_summary,omitempty"`
	Verified            bool      `json:"verified"`
	Confirmed           bool      `json:"confirmed"`
	CreatedAt           time.Time `json:"created_at"`
}

// Action 预设动作（快速操作）
type Action struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Instruction string    `json:"instruction"`
	Shortcut    string    `json:"shortcut,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// VerifiedCase 人工验证后计入数据集的案例，供验证器交叉验证。
type VerifiedCase struct {
	Category  string    `json:"category"`
	Priority  string    `json:"priority"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Message 服务器推送的用户反馈消息（收件箱）。
type Message struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Handled   bool      `json:"handled"`
	TicketID  string    `json:"ticket_id,omitempty"`
}
