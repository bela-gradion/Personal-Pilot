// Package tokens sums one agent session's token usage and appends it to a CSV.
// It reads Claude Code transcripts (~/.claude/projects) and Gemini Antigravity
// transcripts (~/.gemini/antigravity-cli/brain).
package tokens

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Counts holds input, output, and cache token statistics.
type Counts struct {
	SessionID string `json:"session_id"`
	Source    string `json:"source"` // claude | gemini
	Input     int64  `json:"input_tokens"`
	Output    int64  `json:"output_tokens"`
	Cache     int64  `json:"cache_tokens"`
	Total     int64  `json:"total_tokens"`
}

type session struct {
	id, source string
	files      []string
	modTime    time.Time
}

func scanLines(r io.Reader, fn func([]byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(sc.Bytes())
		}
	}
	return sc.Err()
}

// countClaude sums a Claude Code transcript. One API response is written as several
// lines that repeat the same usage, so each message id is counted once. Cache writes
// count as input (new tokens sent); Cache is cache reads, as for Gemini.
func countClaude(r io.Reader, c *Counts, seen map[string]bool) error {
	return scanLines(r, func(line []byte) {
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Usage *struct {
					Input       int64 `json:"input_tokens"`
					CacheCreate int64 `json:"cache_creation_input_tokens"`
					CacheRead   int64 `json:"cache_read_input_tokens"`
					Output      int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" || rec.Message.Usage == nil {
			return
		}
		if id := rec.Message.ID; id != "" {
			if seen[id] {
				return
			}
			seen[id] = true
		}
		u := rec.Message.Usage
		c.Input += u.Input + u.CacheCreate
		c.Output += u.Output
		c.Cache += u.CacheRead
	})
}

func extractGemini(v interface{}, c *Counts) {
	switch val := v.(type) {
	case map[string]interface{}:
		if in, ok := val["input_tokens"].(float64); ok {
			c.Input += int64(in)
		}
		if out, ok := val["output_tokens"].(float64); ok {
			c.Output += int64(out)
		}
		if cache, ok := val["cache_read_tokens"].(float64); ok {
			c.Cache += int64(cache)
		}
		for _, child := range val {
			extractGemini(child, c)
		}
	case []interface{}:
		for _, child := range val {
			extractGemini(child, c)
		}
	}
}

func countGemini(r io.Reader, c *Counts) error {
	return scanLines(r, func(line []byte) {
		var parsed interface{}
		if json.Unmarshal(line, &parsed) == nil {
			extractGemini(parsed, c)
		}
	})
}

// sessions lists every Claude Code and Gemini session on disk.
func sessions(home string) []session {
	var out []session
	mains, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	for _, f := range mains {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		subs, _ := filepath.Glob(filepath.Join(filepath.Dir(f), id, "subagents", "*.jsonl"))
		out = append(out, session{id: id, source: "claude", files: append([]string{f}, subs...), modTime: info.ModTime()})
	}
	gems, _ := filepath.Glob(filepath.Join(home, ".gemini", "antigravity-cli", "brain", "*", ".system_generated", "logs", "transcript.jsonl"))
	for _, f := range gems {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		id := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(f))))
		out = append(out, session{id: id, source: "gemini", files: []string{f}, modTime: info.ModTime()})
	}
	return out
}

// pick returns the session with sessionID, or the most recently written one when sessionID is empty.
func pick(all []session, sessionID string) (session, error) {
	var best session
	for _, s := range all {
		if sessionID != "" && s.id == sessionID {
			return s, nil
		}
		if sessionID == "" && s.modTime.After(best.modTime) {
			best = s
		}
	}
	if best.id == "" {
		if sessionID != "" {
			return best, fmt.Errorf("session %s not found in Claude Code or Gemini transcripts", sessionID)
		}
		return best, fmt.Errorf("no Claude Code or Gemini transcripts found")
	}
	return best, nil
}

// LogTokens sums the token usage of sessionID (latest session when empty), appends it to outputCSV and returns it.
func LogTokens(sessionID, notes, outputCSV string) (*Counts, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user home dir: %w", err)
	}
	s, err := pick(sessions(home), sessionID)
	if err != nil {
		return nil, err
	}

	counts := Counts{SessionID: s.id, Source: s.source}
	seen := map[string]bool{}
	for _, path := range s.files {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open transcript %s: %w", path, err)
		}
		if s.source == "claude" {
			err = countClaude(f, &counts, seen)
		} else {
			err = countGemini(f, &counts)
		}
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("error reading transcript %s: %w", path, err)
		}
	}
	counts.Total = counts.Input + counts.Output + counts.Cache

	if outputCSV == "" {
		return &counts, nil
	}
	if notes == "" {
		notes = "personal-pilot daily run"
	}
	now := time.Now()
	// notes is the last column; strip commas and newlines so the row stays one row.
	notes = strings.NewReplacer(",", ";", "\n", " ").Replace(notes)
	row := fmt.Sprintf("%s,%s,%s,%d,%d,%d,%d,1.0,%s\n",
		now.Format("2006-01-02"), now.Format(time.RFC3339Nano), s.id, counts.Input, counts.Output, counts.Cache, counts.Total, notes)

	if err := os.MkdirAll(filepath.Dir(outputCSV), 0755); err != nil {
		return nil, fmt.Errorf("failed to create CSV directory: %w", err)
	}
	if _, err := os.Stat(outputCSV); os.IsNotExist(err) {
		header := "date,timestamp,session_id,input_tokens,output_tokens,cache_tokens,total_tokens,workflow_version,notes\n"
		if err := os.WriteFile(outputCSV, []byte(header), 0644); err != nil {
			return nil, fmt.Errorf("failed to create CSV file: %w", err)
		}
	}
	f, err := os.OpenFile(outputCSV, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV for append: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(row); err != nil {
		return nil, fmt.Errorf("failed to append row to CSV: %w", err)
	}
	return &counts, nil
}
