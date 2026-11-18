package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"personal-pilot/scripts/pkg/github"
	"personal-pilot/scripts/pkg/tokens"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "github":
		fs := flag.NewFlagSet("github", flag.ExitOnError)
		dateFlag := fs.String("date", "", "Target date (YYYY-MM-DD)")
		prettyFlag := fs.Bool("pretty", false, "Pretty-print JSON output")
		_ = fs.Parse(os.Args[2:])

		res, err := github.Fetch(context.Background(), *dateFlag)
		if err != nil {
			log.Fatalf("GitHub fetch failed: %v", err)
		}
		outBytes, _ := res.RenderJSON(*prettyFlag)
		fmt.Println(string(outBytes))

	case "log-tokens":
		fs := flag.NewFlagSet("log-tokens", flag.ExitOnError)
		sessionID := fs.String("session", "", "Session ID")
		notes := fs.String("notes", "personal-pilot daily run", "Notes description")
		outputCSV := fs.String("output", "data/token_usage.csv", "Output CSV path")
		_ = fs.Parse(os.Args[2:])

		res, err := tokens.LogTokens("", *sessionID, *notes, *outputCSV)
		if err != nil {
			log.Fatalf("Log tokens failed: %v", err)
		}
		fmt.Printf("Logged token usage for session %s (Total: %d tokens)\n", res.SessionID, res.Total)

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: pilot <subcommand> [flags]")
	fmt.Println("Subcommands:")
	fmt.Println("  github      Fetch GitHub PRs and commits")
	fmt.Println("  log-tokens  Track session token usage into CSV")
}
