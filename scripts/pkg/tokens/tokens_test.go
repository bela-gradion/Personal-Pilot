package tokens

import (
	"strings"
	"testing"
	"time"
)

func TestCountClaude(t *testing.T) {
	usage := `"usage":{"input_tokens":2,"cache_creation_input_tokens":10,"cache_read_input_tokens":100,"output_tokens":5,"iterations":[{"input_tokens":2,"output_tokens":5,"cache_read_input_tokens":100}]}`
	transcript := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"id":"m1",` + usage + `}}`,
		`{"type":"assistant","message":{"id":"m1",` + usage + `}}`, // same response, second content block
		`{"type":"assistant","message":{"id":"m2",` + usage + `}}`,
		`not json`,
	}, "\n")
	var c Counts
	if err := countClaude(strings.NewReader(transcript), &c, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if c.Input != 24 || c.Output != 10 || c.Cache != 200 {
		t.Errorf("got input=%d output=%d cache=%d, want 24 10 200", c.Input, c.Output, c.Cache)
	}
}

func TestPick(t *testing.T) {
	now := time.Now()
	all := []session{
		{id: "old-claude", source: "claude", modTime: now.Add(-time.Hour)},
		{id: "new-gemini", source: "gemini", modTime: now},
	}
	if s, _ := pick(all, ""); s.id != "new-gemini" {
		t.Errorf("latest: got %s", s.id)
	}
	if s, _ := pick(all, "old-claude"); s.source != "claude" {
		t.Errorf("by id: got %s", s.source)
	}
	if _, err := pick(all, "missing"); err == nil {
		t.Error("missing id: want error")
	}
}
