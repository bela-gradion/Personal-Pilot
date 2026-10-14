package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"

	"personal-pilot/scripts/pkg/calendar"
	"personal-pilot/scripts/pkg/github"
	"personal-pilot/scripts/pkg/tokens"
)

type CollectedData struct {
	Calendar *calendar.Output `json:"calendar,omitempty"`
	GitHub   *github.Output   `json:"github,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "fetch-github", "github":
		fs := flag.NewFlagSet("fetch-github", flag.ExitOnError)
		dateFlag := fs.String("date", "", "Target date (YYYY-MM-DD)")
		prettyFlag := fs.Bool("pretty", false, "Pretty-print JSON output")
		_ = fs.Parse(os.Args[2:])

		res, err := github.Fetch(context.Background(), *dateFlag)
		if err != nil {
			log.Fatalf("GitHub fetch failed: %v", err)
		}
		outBytes, _ := res.RenderJSON(*prettyFlag)
		fmt.Println(string(outBytes))

	case "fetch-calendar", "calendar":
		fs := flag.NewFlagSet("fetch-calendar", flag.ExitOnError)
		dateFlag := fs.String("date", "", "Target date (YYYY-MM-DD)")
		sourceFlag := fs.String("source", "", "iCal source URL or file path")
		prettyFlag := fs.Bool("pretty", false, "Pretty-print JSON output")
		_ = fs.Parse(os.Args[2:])

		res, err := calendar.Fetch(context.Background(), *sourceFlag, *dateFlag)
		if err != nil {
			log.Fatalf("Calendar fetch failed: %v", err)
		}
		outBytes, _ := res.RenderJSON(*prettyFlag)
		fmt.Println(string(outBytes))

	case "log-tokens", "tokens":
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

	case "collect-all":
		fs := flag.NewFlagSet("collect-all", flag.ExitOnError)
		dateFlag := fs.String("date", "", "Target date (YYYY-MM-DD)")
		sourceFlag := fs.String("source", "", "iCal source URL or file path")
		prettyFlag := fs.Bool("pretty", false, "Pretty-print JSON output")
		_ = fs.Parse(os.Args[2:])

		ctx := context.Background()
		var (
			data CollectedData
			wg   sync.WaitGroup
			mu   sync.Mutex
			errs []error
		)

		wg.Add(2)

		go func() {
			defer wg.Done()
			ghRes, err := github.Fetch(ctx, *dateFlag)
			mu.Lock()
			if err != nil {
				errs = append(errs, fmt.Errorf("github: %w", err))
			} else {
				data.GitHub = ghRes
			}
			mu.Unlock()
		}()

		go func() {
			defer wg.Done()
			calRes, err := calendar.Fetch(ctx, *sourceFlag, *dateFlag)
			mu.Lock()
			if err != nil {
				errs = append(errs, fmt.Errorf("calendar: %w", err))
			} else {
				data.Calendar = calRes
			}
			mu.Unlock()
		}()

		wg.Wait()

		if len(errs) > 0 {
			for _, err := range errs {
				log.Printf("Error: %v", err)
			}
			os.Exit(1)
		}

		var out []byte
		if *prettyFlag {
			out, _ = json.MarshalIndent(data, "", "  ")
		} else {
			out, _ = json.Marshal(data)
		}
		fmt.Println(string(out))

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand %q\n\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: pilot <subcommand> [flags]")
	fmt.Println("Subcommands:")
	fmt.Println("  fetch-github    Fetch GitHub PRs and commits")
	fmt.Println("  fetch-calendar  Fetch iCal events")
	fmt.Println("  log-tokens      Track session token usage into CSV")
	fmt.Println("  collect-all     Fetch both GitHub and Calendar data concurrently")
}
