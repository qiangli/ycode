package fileops

import (
	"testing"
)

func TestValidateReadPath_Safe(t *testing.T) {
	safePaths := []string{
		"/tmp/test.txt",
		"/home/user/file.go",
		"/dev/null",
		"/var/log/syslog",
	}
	for _, path := range safePaths {
		if err := ValidateReadPath(path); err != nil {
			t.Errorf("ValidateReadPath(%q) = %v, want nil", path, err)
		}
	}
}

func TestValidateReadPath_Blocked(t *testing.T) {
	blockedPaths := []string{
		"/dev/zero",
		"/dev/random",
		"/dev/urandom",
		"/dev/full",
		"/dev/stdin",
		"/dev/tty",
		"/dev/console",
		"/dev/stdout",
		"/dev/stderr",
	}
	for _, path := range blockedPaths {
		if err := ValidateReadPath(path); err == nil {
			t.Errorf("ValidateReadPath(%q) should be blocked", path)
		}
	}
}

func TestValidateReadPath_ProcFD(t *testing.T) {
	if err := ValidateReadPath("/proc/123/fd/0"); err == nil {
		t.Error("/proc/*/fd/0 should be blocked")
	}
	if err := ValidateReadPath("/proc/1/fd/1"); err == nil {
		t.Error("/proc/*/fd/1 should be blocked")
	}
	if err := ValidateReadPath("/proc/99/fd/2"); err == nil {
		t.Error("/proc/*/fd/2 should be blocked")
	}
	// fd/3+ should be allowed (not stdio).
	if err := ValidateReadPath("/proc/123/fd/3"); err != nil {
		t.Errorf("/proc/*/fd/3 should be allowed: %v", err)
	}
}

func TestValidateReadPath_GeneralDev(t *testing.T) {
	if err := ValidateReadPath("/dev/something_unknown"); err == nil {
		t.Error("unknown /dev/ paths should be blocked")
	}
}
