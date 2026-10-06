package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type TokenCounts struct {
	Input  int64
	Output int64
	Cache  int64
}

func extractTokens(v interface{}, counts *TokenCounts) {
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

func findLatestSession(brainDir string) (string, error) {
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return "", err
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
		// Verify transcript exists
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

func main() {
	sessionID := flag.String("session", "", "Session ID (defaults to latest modified session)")
	notes := flag.String("notes", "personal-pilot daily run", "Notes description for token tracking")
	outputCSV := flag.String("output", "data/token_usage.csv", "Path to CSV output file")
	flag.Parse()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to resolve user home dir: %v", err)
	}
	brainDir := filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain")

	sid := *sessionID
	if sid == "" {
		detected, err := findLatestSession(brainDir)
		if err != nil {
			log.Fatalf("Failed to auto-detect session: %v", err)
		}
		sid = detected
	}

	transcriptPath := filepath.Join(brainDir, sid, ".system_generated", "logs", "transcript.jsonl")
	file, err := os.Open(transcriptPath)
	if err != nil {
		log.Fatalf("Failed to open transcript at %s: %v", transcriptPath, err)
	}
	defer file.Close()

	var counts TokenCounts
	scanner := bufio.NewScanner(file)
	// Allow scanning lines up to 10MB
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
		log.Fatalf("Error reading transcript: %v", err)
	}

	total := counts.Input + counts.Output + counts.Cache
	now := time.Now()
	dateStr := now.Format("2006-01-02")
	isoTimestamp := now.Format(time.RFC3339Nano)

	// Format row matching schema:
	// date,timestamp,session_id,input_tokens,output_tokens,cache_tokens,total_tokens,workflow_version,notes
	csvRow := fmt.Sprintf("%s,%s,%s,%d,%d,%d,%d,1.0,%s\n",
		dateStr, isoTimestamp, sid, counts.Input, counts.Output, counts.Cache, total, *notes)

	// Check if CSV exists, if not write header
	if _, err := os.Stat(*outputCSV); os.IsNotExist(err) {
		header := "date,timestamp,session_id,input_tokens,output_tokens,cache_tokens,total_tokens,workflow_version,notes\n"
		if err := os.WriteFile(*outputCSV, []byte(header), 0644); err != nil {
			log.Fatalf("Failed to create CSV file: %v", err)
		}
	}

	f, err := os.OpenFile(*outputCSV, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open CSV for append: %v", err)
	}
	defer f.Close()

	if _, err := f.WriteString(csvRow); err != nil {
		log.Fatalf("Failed to append row to CSV: %v", err)
	}

	fmt.Printf("Logged token usage for session %s:\n", sid)
	fmt.Printf("  Input tokens:      %d\n", counts.Input)
	fmt.Printf("  Output tokens:     %d\n", counts.Output)
	fmt.Printf("  Cache read tokens: %d\n", counts.Cache)
	fmt.Printf("  Total tokens:      %d\n", total)
	fmt.Printf("  Saved to:          %s\n", *outputCSV)
}
