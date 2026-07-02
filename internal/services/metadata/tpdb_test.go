package metadata

import (
	"testing"
	"time"
)

func TestTPDBRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"", time.Second},
		{"5", 5 * time.Second},
		{"60", 30 * time.Second},
		{"  2  ", 2 * time.Second},
		{"bad", time.Second},
	}
	for _, tc := range tests {
		if got := tpdbRetryAfter(tc.header); got != tc.want {
			t.Errorf("tpdbRetryAfter(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
