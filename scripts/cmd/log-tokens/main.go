package main

import (
	"flag"
	"fmt"
	"log"

	"personal-pilot/scripts/pkg/tokens"
)

func main() {
	sessionID := flag.String("session", "", "Session ID (defaults to latest modified session)")
	notes := flag.String("notes", "personal-pilot daily run", "Notes description for token tracking")
	outputCSV := flag.String("output", "data/token_usage.csv", "Path to CSV output file")
	flag.Parse()

	res, err := tokens.LogTokens("", *sessionID, *notes, *outputCSV)
	if err != nil {
		log.Fatalf("log-tokens failed: %v", err)
	}

	fmt.Printf("Logged token usage for session %s:\n", res.SessionID)
	fmt.Printf("  Input tokens:      %d\n", res.Input)
	fmt.Printf("  Output tokens:     %d\n", res.Output)
	fmt.Printf("  Cache read tokens: %d\n", res.Cache)
	fmt.Printf("  Total tokens:      %d\n", res.Total)
	fmt.Printf("  Saved to:          %s\n", *outputCSV)
}
