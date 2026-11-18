package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"personal-pilot/scripts/pkg/calendar"
)

func main() {
	dateFlag := flag.String("date", "", "Target date in YYYY-MM-DD format (defaults to today)")
	sourceFlag := flag.String("source", "", "iCal source: URL (https://…) or local file path (overrides ICAL_FEED_URL env var)")
	prettyFlag := flag.Bool("pretty", false, "Output pretty-printed JSON (defaults to compact JSON)")
	flag.Parse()

	res, err := calendar.Fetch(context.Background(), *sourceFlag, *dateFlag)
	if err != nil {
		log.Fatalf("fetch-calendar failed: %v", err)
	}

	outBytes, err := res.RenderJSON(*prettyFlag)
	if err != nil {
		log.Fatalf("failed to marshal calendar JSON: %v", err)
	}

	fmt.Fprintln(os.Stdout, string(outBytes))
}
