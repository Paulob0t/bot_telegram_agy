package utils

import "testing"

func TestFormatUptime(t *testing.T) {
	if got := FormatUptime(45); got != "45s" {
		t.Fatalf("got %q, want 45s", got)
	}
	if got := FormatUptime(125); got != "2m 5s" {
		t.Fatalf("got %q, want 2m 5s", got)
	}
	if got := FormatUptime(3660); got != "1h 1m" {
		t.Fatalf("got %q, want 1h 1m", got)
	}
}

func TestSanitizeStreamingMarkdown(t *testing.T) {
	in := "hola\n```go\nfmt.Println(1)"
	got := SanitizeStreamingMarkdown(in, 3800)
	if got != in+"\n```" {
		t.Fatalf("expected closed fence, got %q", got)
	}
}
