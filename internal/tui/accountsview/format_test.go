package accountsview

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSpan(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:             "30s",
		5 * time.Minute:              "5m",
		90 * time.Minute:             "1h30m",
		12 * time.Hour:               "12h",
		3 * 24 * time.Hour:           "3d",
		10*time.Hour + 5*time.Minute: "10h",
	}
	for d, want := range cases {
		if got := span(d); got != want {
			t.Errorf("span(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestUntilAndAgo(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if got := until(now, now.Add(54*time.Minute)); got != "in 54m" {
		t.Errorf("until = %q", got)
	}
	if got := until(now, now.Add(-3*time.Minute)); got != "expired 3m ago" {
		t.Errorf("until past = %q", got)
	}
	if got := ago(now, now.Add(-2*time.Hour)); got != "2h ago" {
		t.Errorf("ago = %q", got)
	}
	if ago(now, time.Time{}) != "never" || until(now, time.Time{}) != "unknown" {
		t.Error("a zero time must read as never or unknown")
	}
}

func TestSparkScalesAndKeepsLightUseVisible(t *testing.T) {
	counts := make([]int, 144)
	counts[143] = 100
	counts[0] = 1
	s := spark(counts, 48)
	if utf8.RuneCountInString(s) != 48 {
		t.Fatalf("spark is %d cells, want 48", utf8.RuneCountInString(s))
	}
	runes := []rune(s)
	if runes[47] != '█' {
		t.Errorf("the busiest window must be a full block: %q", s)
	}
	if runes[0] == ' ' {
		t.Errorf("one request next to a burst must still show: %q", s)
	}
	if strings.Trim(spark(make([]int, 144), 10), " ") != "" {
		t.Error("no requests must draw nothing")
	}
}

func TestWrapKeepsEveryCharacter(t *testing.T) {
	url := "https://claude.ai/oauth/authorize?code=true&state=abcdef"
	lines := wrap(url, 20)
	if strings.Join(lines, "") != url {
		t.Errorf("wrap lost characters: %v", lines)
	}
	for _, l := range lines {
		if utf8.RuneCountInString(l) > 20 {
			t.Errorf("line %q is longer than 20", l)
		}
	}
}
