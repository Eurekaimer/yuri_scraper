package sites

import (
	"errors"
	"strings"
	"testing"
)

func TestDiagnoseError_Timeout(t *testing.T) {
	err := diagnoseError(errors.New("context deadline exceeded"), "")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "超时") {
		t.Errorf("expected timeout message, got: %s", msg)
	}
}

func TestDiagnoseError_ConnectionRefused(t *testing.T) {
	err := diagnoseError(errors.New("connection refused"), "")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "无法连接") {
		t.Errorf("expected connection refused message, got: %s", msg)
	}
}

func TestDiagnoseError_NoSuchHost(t *testing.T) {
	err := diagnoseError(errors.New("no such host"), "")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "DNS") {
		t.Errorf("expected DNS message, got: %s", msg)
	}
}

func TestDiagnoseError_ConnectionReset(t *testing.T) {
	err := diagnoseError(errors.New("connection reset by peer"), "")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "重置") {
		t.Errorf("expected reset message, got: %s", msg)
	}
	if !strings.Contains(msg, "速度") {
		t.Errorf("expected speed suggestion, got: %s", msg)
	}
}

func TestDiagnoseError_Generic(t *testing.T) {
	orig := errors.New("something unknown happened")
	err := diagnoseError(orig, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, orig) {
		t.Error("generic error should wrap original")
	}
}
