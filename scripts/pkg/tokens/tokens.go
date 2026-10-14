package tokens

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Counts holds input, output, and cache token statistics.
type Counts struct {
	SessionID string `json:"session_id"`
	Input     int64  `json:"input_tokens"`
	Output    int64  `json:"output_tokens"`
	Cache     int64  `json:"cache_tokens"`
	Total     int64  `json:"total_tokens"`
}

func extractTokens(v interface{}, counts *Counts) {
	switch val := v.(type) {
	case map[string]interface{}:
		if in, ok := val["input_tokens"].(float64); ok {
			counts.Input += int64(in)
		}
		if out, ok := val["output_tokens"].(float64); ok {
			counts.Output += int64(out)
		}
		if cache, ok := val["cache_read_tokens"].(float64); ok {
			counts.Cache += int64(cache)
		}
		for _, child := range val {
			extractTokens(child, counts)
		}
	case []interface{}:
		for _, child := range val {
			extractTokens(child, counts)
		}
	}
}

// FindLatestSession finds the ID of the most recently modified transcript session in brainDir.
func FindLatestSession(brainDir string) (string, error) {
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return "", fmt.Errorf("failed to read brain directory %s: %w", brainDir, err)
	}

	type sessionDir struct {
		id      string
		modTime time.Time
	}
	var dirs []sessionDir

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		transcriptPath := filepath.Join(brainDir, entry.Name(), ".system_generated", "logs", "transcript.jsonl")
		if _, err := os.Stat(transcriptPath); err == nil {
			dirs = append(dirs, sessionDir{id: entry.Name(), modTime: info.ModTime()})
		}
	}

	if len(dirs) == 0 {
		return "", fmt.Errorf("no valid session directory found in %s", brainDir)
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.After(dirs[j].modTime)
	})

	return dirs[0].id, nil
}

// LogTokens parses transcript logs for sessionID, appends token usage to outputCSV, and returns Counts.
func LogTokens(brainDir, sessionID, notes, outputCSV string) (*Counts, error) {
	if brainDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve user home dir: %w", err)
		}
		brainDir = filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain")
	}

	if sessionID == "" {
		detected, err := FindLatestSession(brainDir)
		if err != nil {
			return nil, fmt.Errorf("failed to auto-detect session: %w", err)
		}
		sessionID = detected
	}

	transcriptPath := filepath.Join(brainDir, sessionID, ".system_generated", "logs", "transcript.jsonl")
	file, err := os.Open(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open transcript at %s: %w", transcriptPath, err)
	}
	defer file.Close()

	counts := Counts{SessionID: sessionID}
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var parsed interface{}
		if err := json.Unmarshal(line, &parsed); err != nil {
			continue
		}
		extractTokens(parsed, &counts)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading transcript: %w", err)
	}

	counts.Total = counts.Input + counts.Output + counts.Cache

	if outputCSV != "" {
		now := time.Now()
		dateStr := now.Format("2006-01-02")
		isoTimestamp := now.Format(time.RFC3339Nano)

		if notes == "" {
			notes = "personal-pilot daily run"
		}

		csvRow := fmt.Sprintf("%s,%s,%s,%d,%d,%d,%d,1.0,%s\n",
			dateStr, isoTimestamp, sessionID, counts.Input, counts.Output, counts.Cache, counts.Total, notes)

		// Ensure parent directory exists
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

		if _, err := f.WriteString(csvRow); err != nil {
			return nil, fmt.Errorf("failed to append row to CSV: %w", err)
		}
	}

	return &counts, nil
}
