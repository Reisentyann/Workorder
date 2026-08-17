package sanitize

import "regexp"

var (
	phoneRe = regexp.MustCompile(`1[3-9]\d{9}`)
	idRe    = regexp.MustCompile(`\d{17}[\dXx]`)
	emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	cardRe  = regexp.MustCompile(`\d{13,19}`)
)

// Mask 对文本中的敏感信息做脱敏（手机号/身份证/邮箱/银行卡）。
func Mask(s string) string {
	s = phoneRe.ReplaceAllStringFunc(s, maskPhone)
	s = idRe.ReplaceAllStringFunc(s, maskID)
	s = emailRe.ReplaceAllStringFunc(s, maskEmail)
	s = cardRe.ReplaceAllStringFunc(s, func(v string) string { return "****" })
	return s
}

func maskPhone(s string) string {
	if len(s) != 11 {
		return "****"
	}
	return s[:3] + "****" + s[7:]
}

func maskID(s string) string {
	if len(s) < 8 {
		return "****"
	}
	return s[:6] + "****" + s[len(s)-4:]
}

func maskEmail(s string) string {
	at := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			at = i
			break
		}
	}
	if at <= 0 {
		return "***"
	}
	return s[:1] + "***@" + s[at+1:]
}
