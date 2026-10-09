package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// CalendarEvent holds the parsed fields for a single VEVENT.
type CalendarEvent struct {
	Title     string   `json:"title"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	Attendees []string `json:"attendees,omitempty"`
}

// CalendarOutput is the top-level JSON envelope, mirroring the shape of fetch-github output.
type CalendarOutput struct {
	Date   string          `json:"date"`
	Events []CalendarEvent `json:"events"`
}

// icalTimeLayouts lists all iCal datetime formats defined in RFC 5545 that we handle.
// Order matters: most-specific (with timezone ID prefix) first.
var icalTimeLayouts = []string{
	"20060102T150405Z",     // UTC form
	"20060102T150405",      // floating / local form
	"20060102",             // date-only (all-day)
}

// parseICalTime converts an iCal DTSTART/DTEND property value (and optional TZID parameter)
// into a time.Time using the target location.
func parseICalTime(value, tzid string, loc *time.Location) (time.Time, error) {
	// Strip any leading TZID=… prefix that may have been included in the value itself.
	if idx := strings.Index(value, ":"); idx != -1 {
		value = value[idx+1:]
	}

	// Determine the parsing location: UTC for values ending in 'Z', named TZ if given, else local.
	parseLoc := loc
	if strings.HasSuffix(value, "Z") {
		parseLoc = time.UTC
	} else if tzid != "" {
		if tz, err := time.LoadLocation(tzid); err == nil {
			parseLoc = tz
		}
	}

	for _, layout := range icalTimeLayouts {
		t, err := time.ParseInLocation(layout, value, parseLoc)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised iCal time value %q", value)
}

// extractTZID pulls the TZID parameter value from a property line such as
// "DTSTART;TZID=Asia/Bangkok:20261009T090000".
func extractTZID(propLine string) string {
	paramSection := strings.SplitN(propLine, ":", 2)[0]
	for _, param := range strings.Split(paramSection, ";") {
		if strings.HasPrefix(strings.ToUpper(param), "TZID=") {
			return param[5:]
		}
	}
	return ""
}

// extractPropValue returns the value portion of a property line (everything after the first colon).
func extractPropValue(line string) string {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return line[idx+1:]
}

// propName returns the property name (uppercased, before any ';' or ':').
func propName(line string) string {
	name := line
	if idx := strings.IndexAny(name, ";:"); idx >= 0 {
		name = name[:idx]
	}
	return strings.ToUpper(strings.TrimSpace(name))
}

// unfoldLines joins RFC 5545 "folded" continuation lines (a line beginning with a space or tab
// is a continuation of the previous line).
func unfoldLines(r io.Reader) []string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var lines []string
	for scanner.Scan() {
		raw := scanner.Text()
		// Continuation lines start with a single space or tab (RFC 5545 §3.1).
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += raw[1:]
		} else {
			lines = append(lines, raw)
		}
	}
	return lines
}

// parseICS reads an iCal stream and returns all events that fall on targetDate in loc.
func parseICS(r io.Reader, targetDate string, loc *time.Location) ([]CalendarEvent, error) {
	lines := unfoldLines(r)

	var events []CalendarEvent
	inEvent := false

	var (
		summaryLine   string
		dtStartLine   string
		dtEndLine     string
		attendeeLines []string
	)

	for _, line := range lines {
		name := propName(line)

		switch name {
		case "BEGIN":
			if strings.EqualFold(extractPropValue(line), "VEVENT") {
				inEvent = true
				summaryLine = ""
				dtStartLine = ""
				dtEndLine = ""
				attendeeLines = nil
			}

		case "END":
			if strings.EqualFold(extractPropValue(line), "VEVENT") && inEvent {
				inEvent = false

				// Parse start time.
				startVal := extractPropValue(dtStartLine)
				startTZID := extractTZID(dtStartLine)
				startTime, err := parseICalTime(startVal, startTZID, loc)
				if err != nil {
					log.Printf("Skipping event with unparseable DTSTART %q: %v", startVal, err)
					continue
				}

				// Parse end time (optional; default to start time if missing).
				var endTime time.Time
				if dtEndLine != "" {
					endVal := extractPropValue(dtEndLine)
					endTZID := extractTZID(dtEndLine)
					endTime, err = parseICalTime(endVal, endTZID, loc)
					if err != nil {
						log.Printf("Skipping event with unparseable DTEND %q: %v", endVal, err)
						continue
					}
				} else {
					endTime = startTime
				}

				// Convert to the target location for date comparison.
				startLocal := startTime.In(loc)
				if startLocal.Format("2006-01-02") != targetDate {
					continue
				}

				title := decodeICalText(extractPropValue(summaryLine))

				var attendees []string
				for _, al := range attendeeLines {
					a := extractAttendeeDisplayName(al)
					if a != "" {
						attendees = append(attendees, a)
					}
				}

				events = append(events, CalendarEvent{
					Title:     title,
					Start:     startTime.In(loc).Format("15:04"),
					End:       endTime.In(loc).Format("15:04"),
					Attendees: attendees,
				})
			}

		default:
			if !inEvent {
				continue
			}
			switch name {
			case "SUMMARY":
				summaryLine = line
			case "DTSTART":
				dtStartLine = line
			case "DTEND":
				dtEndLine = line
			case "ATTENDEE":
				attendeeLines = append(attendeeLines, line)
			}
		}
	}

	// Sort events chronologically by start time.
	sort.Slice(events, func(i, j int) bool {
		return events[i].Start < events[j].Start
	})

	return events, nil
}

// decodeICalText unescapes RFC 5545 TEXT value characters (\\, \,, \n, \N, \;).
func decodeICalText(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\N`, "\n")
	s = strings.ReplaceAll(s, `\,`, ",")
	s = strings.ReplaceAll(s, `\;`, ";")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return strings.TrimSpace(s)
}

// extractAttendeeDisplayName prefers the CN parameter value; falls back to the mailto: address.
func extractAttendeeDisplayName(line string) string {
	// e.g. ATTENDEE;CN=Alice Smith;RSVP=TRUE:mailto:alice@example.com
	paramSection := strings.SplitN(line, ":", 2)[0]
	for _, param := range strings.Split(paramSection, ";") {
		if strings.HasPrefix(strings.ToUpper(param), "CN=") {
			return strings.TrimSpace(param[3:])
		}
	}
	// Fall back to the value (mailto: address).
	val := extractPropValue(line)
	val = strings.TrimPrefix(val, "mailto:")
	val = strings.TrimPrefix(val, "MAILTO:")
	return strings.TrimSpace(val)
}

// openSource fetches the iCal content from a URL (http/https) or opens a local file.
func openSource(source string) (io.ReadCloser, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		resp, err := http.Get(source) //nolint:gosec // user-supplied trusted calendar URL
		if err != nil {
			return nil, fmt.Errorf("failed to fetch iCal URL %q: %w", source, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d fetching iCal URL %q", resp.StatusCode, source)
		}
		return resp.Body, nil
	}

	f, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("failed to open iCal file %q: %w", source, err)
	}
	return f, nil
}

func main() {
	dateFlag := flag.String("date", "", "Target date in YYYY-MM-DD format (defaults to today)")
	sourceFlag := flag.String("source", "", "iCal source: URL (https://…) or local file path (overrides ICAL_FEED_URL env var)")
	prettyFlag := flag.Bool("pretty", false, "Output pretty-printed JSON (defaults to compact JSON)")
	flag.Parse()

	// Resolve target date.
	targetDate := *dateFlag
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	} else {
		if _, err := time.Parse("2006-01-02", targetDate); err != nil {
			log.Fatalf("Invalid date format %q: expected YYYY-MM-DD", targetDate)
		}
	}

	// Resolve iCal source: flag > env var.
	source := *sourceFlag
	if source == "" {
		source = os.Getenv("ICAL_FEED_URL")
	}
	if source == "" {
		log.Fatalf("No iCal source specified. Provide -source <url|path> or set the ICAL_FEED_URL environment variable.")
	}

	// Use the local timezone for date comparisons and time formatting.
	loc := time.Local

	rc, err := openSource(source)
	if err != nil {
		log.Fatalf("Failed to open iCal source: %v", err)
	}
	defer rc.Close()

	events, err := parseICS(rc, targetDate, loc)
	if err != nil {
		log.Fatalf("Failed to parse iCal data: %v", err)
	}

	output := CalendarOutput{
		Date:   targetDate,
		Events: events,
	}

	var outBytes []byte
	if *prettyFlag {
		outBytes, err = json.MarshalIndent(output, "", "  ")
	} else {
		outBytes, err = json.Marshal(output)
	}
	if err != nil {
		log.Fatalf("Failed to marshal output JSON: %v", err)
	}

	fmt.Println(string(outBytes))
}
