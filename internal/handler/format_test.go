package handler

import "testing"

func TestTruncateIsRuneSafe(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"Исправлена ошибка", 10, "Исправл..."},
		{"abc", 2, "ab"},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.max); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestGitHelpers(t *testing.T) {
	if got := firstLine("  feat: x\n\nbody"); got != "feat: x" {
		t.Errorf("firstLine: got %q", got)
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Errorf("shortSHA short: got %q", got)
	}
	if got := shortSHA("0123456789abcdef"); got != "0123456" {
		t.Errorf("shortSHA: got %q", got)
	}
	if got := branchName("refs/heads/feature/x"); got != "feature/x" {
		t.Errorf("branchName heads: got %q", got)
	}
	if got := branchName("refs/tags/v1.0.0"); got != "v1.0.0" {
		t.Errorf("branchName tags: got %q", got)
	}
	if !isZeroSHA("0000000000000000000000000000000000000000") || isZeroSHA("") || isZeroSHA("a000") {
		t.Error("isZeroSHA returned wrong result")
	}
}
