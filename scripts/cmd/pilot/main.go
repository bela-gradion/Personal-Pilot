package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"personal-pilot/scripts/pkg/github"
	"personal-pilot/scripts/pkg/tokens"
)

// repoRoot is the personal-pilot checkout, found from this source file so paths
// do not depend on the directory pilot is run from.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "github":
		fs := flag.NewFlagSet("github", flag.ExitOnError)
		dateFlag := fs.String("date", "", "Target date (YYYY-MM-DD, local time; defaults to today)")
		configFlag := fs.String("config", filepath.Join(repoRoot(), "projects.json"), "Repo rules (projects.json)")
		prettyFlag := fs.Bool("pretty", false, "Pretty-print JSON output")
		_ = fs.Parse(os.Args[2:])

		cfg, err := github.LoadConfig(*configFlag)
		if err != nil {
			log.Fatalf("Config: %v", err)
		}
		res, err := github.Fetch(context.Background(), *dateFlag, cfg)
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
	fmt.Println("  github      Fetch the active gh account's PRs and commits for one day.")
	fmt.Println("              Covers only the logged-in account: run it once per gh CLI account")
	fmt.Println("              (gh auth switch --user <login>) and combine the outputs.")
	fmt.Println("  log-tokens  Track session token usage into CSV")
}
