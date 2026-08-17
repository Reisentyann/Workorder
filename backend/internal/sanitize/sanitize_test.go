package sanitize

import (
	"strings"
	"testing"
)

func TestMaskPhone(t *testing.T) {
	out := Mask("请联系 13812345678")
	if strings.Contains(out, "13812345678") {
		t.Errorf("phone not masked: %s", out)
	}
	if !strings.Contains(out, "****") {
		t.Errorf("expected mask marker: %s", out)
	}
}

func TestMaskID(t *testing.T) {
	out := Mask("身份证 110101199001011234")
	if strings.Contains(out, "110101199001011234") {
		t.Errorf("id not masked: %s", out)
	}
}

func TestMaskEmail(t *testing.T) {
	out := Mask("邮箱 test@example.com")
	if strings.Contains(out, "test@example.com") {
		t.Errorf("email not masked: %s", out)
	}
}

func TestMaskKeepsNormalText(t *testing.T) {
	out := Mask("申请退款 800 元，订单号 123456")
	if !strings.Contains(out, "退款") {
		t.Errorf("normal text changed: %s", out)
	}
}
