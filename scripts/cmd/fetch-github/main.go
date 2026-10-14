package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"personal-pilot/scripts/pkg/github"
)

func main() {
	dateFlag := flag.String("date", "", "Target date in YYYY-MM-DD format (defaults to today)")
	prettyFlag := flag.Bool("pretty", false, "Output pretty-printed JSON (defaults to compact JSON)")
	flag.Parse()

	res, err := github.Fetch(context.Background(), *dateFlag)
	if err != nil {
		log.Fatalf("fetch-github failed: %v", err)
	}

	outBytes, err := res.RenderJSON(*prettyFlag)
	if err != nil {
		log.Fatalf("failed to marshal github JSON: %v", err)
	}

	fmt.Fprintln(os.Stdout, string(outBytes))
}
