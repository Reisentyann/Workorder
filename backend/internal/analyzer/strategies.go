package analyzer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Strategy 策略接口：每种工单类型一个实现。新增类型只增不改（开闭原则）。
type Strategy interface {
	Type() string
	Keywords() []string
	Assignee() string
	AutoFixable() bool
	AutoFixSuggestion(content string) string
	// RequiredFields 关键信息字段，供验证器做信息完整性校验。
	RequiredFields() []string
	// RequiredFieldMatchers 每个关键字段的匹配词（含别名），与 RequiredFields 一一对应。
	RequiredFieldMatchers() [][]string
	Priority(content string) string
	Summary(title, content string) string
}

// 金额敏感阈值：退款金额超过该值判高优先级。
const highAmountThreshold = 500

var amountRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:元|块|¥|￥|rmb|RMB)`)

// extractAmount 从正文抽取金额数字（元）。
func extractAmount(s string) float64 {
	m := amountRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	return v
}

// priorityByKeywords 通用优先级：命中强调词则高，否则默认。
func priorityByKeywords(content string, emphasis []string, def string) string {
	for _, k := range emphasis {
		if strings.Contains(content, k) {
			return "高"
		}
	}
	return def
}

/* ---------- 退款申请 ---------- */

type RefundStrategy struct{}

func (*RefundStrategy) Type() string      { return "退款申请" }
func (*RefundStrategy) Assignee() string  { return "退款专员" }
func (*RefundStrategy) AutoFixable() bool { return false }
func (*RefundStrategy) AutoFixSuggestion(string) string {
	return ""
}
func (*RefundStrategy) RequiredFields() []string { return []string{"订单号", "金额"} }
func (*RefundStrategy) RequiredFieldMatchers() [][]string {
	return [][]string{
		{"订单号", "订单", "单号", "order"},
		{"金额", "元", "块", "¥", "￥", "rmb", "RMB"},
	}
}
func (*RefundStrategy) Keywords() []string {
	return []string{"退款", "退货", "返款", "退钱", "退差价", "取消订单", "不想要了"}
}
func (*RefundStrategy) Priority(content string) string {
	if amount := extractAmount(content); amount > highAmountThreshold {
		return "高" // 金额敏感
	}
	return priorityByKeywords(content, []string{"投诉", "多次", "尽快", "催"}, "中")
}
func (*RefundStrategy) Summary(title, content string) string {
	amt := extractAmount(content)
	if amt > 0 {
		return fmt.Sprintf("用户申请退款（金额 %.0f 元）：%s", amt, fmtSummary(title))
	}
	return "用户申请退款：" + fmtSummary(title)
}

/* ---------- 登录异常 ---------- */

type LoginStrategy struct{}

func (*LoginStrategy) Type() string      { return "登录异常" }
func (*LoginStrategy) Assignee() string  { return "技术运维" }
func (*LoginStrategy) AutoFixable() bool { return true }
func (*LoginStrategy) AutoFixSuggestion(string) string {
	return "可自动引导：发送验证码或提供自助重置密码链接，附操作步骤。"
}
func (*LoginStrategy) RequiredFields() []string { return []string{"账号", "验证码"} }
func (*LoginStrategy) RequiredFieldMatchers() [][]string {
	return [][]string{
		{"账号", "帐号", "用户名", "手机号"},
		{"验证码"},
	}
}
func (*LoginStrategy) Keywords() []string {
	return []string{"登录", "登陆", "密码", "验证码", "账号异常", "无法登录", "登不上", "锁定", "冻结"}
}
func (*LoginStrategy) Priority(string) string { return "中" }
func (*LoginStrategy) Summary(title, _ string) string {
	return "用户反馈登录异常：" + fmtSummary(title)
}

/* ---------- 发票问题 ---------- */

type InvoiceStrategy struct{}

func (*InvoiceStrategy) Type() string      { return "发票问题" }
func (*InvoiceStrategy) Assignee() string  { return "财务" }
func (*InvoiceStrategy) AutoFixable() bool { return false }
func (*InvoiceStrategy) AutoFixSuggestion(string) string {
	return ""
}
func (*InvoiceStrategy) RequiredFields() []string { return []string{"抬头", "税号"} }
func (*InvoiceStrategy) RequiredFieldMatchers() [][]string {
	return [][]string{{"抬头"}, {"税号"}}
}
func (*InvoiceStrategy) Keywords() []string {
	return []string{"发票", "开票", "税号", "抬头", "报销", "电子发票"}
}
func (*InvoiceStrategy) Priority(string) string { return "中" }
func (*InvoiceStrategy) Summary(title, _ string) string {
	return "用户反馈发票问题：" + fmtSummary(title)
}

/* ---------- 物流投诉 ---------- */

type LogisticsStrategy struct{}

func (*LogisticsStrategy) Type() string      { return "物流投诉" }
func (*LogisticsStrategy) Assignee() string  { return "物流客服" }
func (*LogisticsStrategy) AutoFixable() bool { return false }
func (*LogisticsStrategy) AutoFixSuggestion(string) string {
	return ""
}
func (*LogisticsStrategy) RequiredFields() []string { return []string{"运单号", "物流"} }
func (*LogisticsStrategy) RequiredFieldMatchers() [][]string {
	return [][]string{
		{"运单号", "运单", "单号", "快递单"},
		{"物流", "快递", "快递公司"},
	}
}
func (*LogisticsStrategy) Keywords() []string {
	return []string{"物流", "快递", "发货", "配送", "丢件", "破损", "迟迟未到", "运单", "没收到货"}
}
func (*LogisticsStrategy) Priority(content string) string {
	return priorityByKeywords(content, []string{"投诉", "破损", "丢件", "多次", "催"}, "中")
}
func (*LogisticsStrategy) Summary(title, _ string) string {
	return "用户反馈物流投诉：" + fmtSummary(title)
}

/* ---------- 兜底：其他 ---------- */

type DefaultStrategy struct{}

func (*DefaultStrategy) Type() string      { return "其他" }
func (*DefaultStrategy) Assignee() string  { return "综合客服" }
func (*DefaultStrategy) AutoFixable() bool { return false }
func (*DefaultStrategy) AutoFixSuggestion(string) string {
	return ""
}
func (*DefaultStrategy) RequiredFields() []string       { return nil }
func (*DefaultStrategy) RequiredFieldMatchers() [][]string { return nil }
func (*DefaultStrategy) Keywords() []string       { return nil }
func (*DefaultStrategy) Priority(string) string   { return "低" }
func (*DefaultStrategy) Summary(title, _ string) string {
	return "工单待人工判断：" + fmtSummary(title)
}
