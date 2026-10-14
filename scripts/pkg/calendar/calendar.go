package calendar

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Event holds the parsed fields for a single VEVENT.
type Event struct {
	Title     string   `json:"title"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	Attendees []string `json:"attendees,omitempty"`
}

// Output is the top-level JSON envelope.
type Output struct {
	Date   string  `json:"date"`
	Events []Event `json:"events"`
}

var icalTimeLayouts = []string{
	"20060102T150405Z", // UTC form
	"20060102T150405",  // floating / local form
	"20060102",         // date-only (all-day)
}

func parseICalTime(value, tzid string, loc *time.Location) (time.Time, error) {
	if idx := strings.Index(value, ":"); idx != -1 {
		value = value[idx+1:]
	}

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

func extractTZID(propLine string) string {
	paramSection := strings.SplitN(propLine, ":", 2)[0]
	for _, param := range strings.Split(paramSection, ";") {
		if strings.HasPrefix(strings.ToUpper(param), "TZID=") {
			return param[5:]
		}
	}
	return ""
}

func extractPropValue(line string) string {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return line[idx+1:]
}

func propName(line string) string {
	name := line
	if idx := strings.IndexAny(name, ";:"); idx >= 0 {
		name = name[:idx]
	}
	return strings.ToUpper(strings.TrimSpace(name))
}

func unfoldLines(r io.Reader) []string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var lines []string
	for scanner.Scan() {
		raw := scanner.Text()
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += raw[1:]
		} else {
			lines = append(lines, raw)
		}
	}
	return lines
}

func parseICS(r io.Reader, targetDate string, loc *time.Location) ([]Event, error) {
	lines := unfoldLines(r)

	var events []Event
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

				startVal := extractPropValue(dtStartLine)
				startTZID := extractTZID(dtStartLine)
				startTime, err := parseICalTime(startVal, startTZID, loc)
				if err != nil {
					continue
				}

				var endTime time.Time
				if dtEndLine != "" {
					endVal := extractPropValue(dtEndLine)
					endTZID := extractTZID(dtEndLine)
					endTime, err = parseICalTime(endVal, endTZID, loc)
					if err != nil {
						continue
					}
				} else {
					endTime = startTime
				}

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

				events = append(events, Event{
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

	sort.Slice(events, func(i, j int) bool {
		return events[i].Start < events[j].Start
	})

	return events, nil
}

func decodeICalText(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\N`, "\n")
	s = strings.ReplaceAll(s, `\,`, ",")
	s = strings.ReplaceAll(s, `\;`, ";")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return strings.TrimSpace(s)
}

func extractAttendeeDisplayName(line string) string {
	paramSection := strings.SplitN(line, ":", 2)[0]
	for _, param := range strings.Split(paramSection, ";") {
		if strings.HasPrefix(strings.ToUpper(param), "CN=") {
			return strings.TrimSpace(param[3:])
		}
	}
	val := extractPropValue(line)
	val = strings.TrimPrefix(val, "mailto:")
	val = strings.TrimPrefix(val, "MAILTO:")
	return strings.TrimSpace(val)
}

func openSource(ctx context.Context, source string) (io.ReadCloser, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request for iCal URL %q: %w", source, err)
		}
		resp, err := http.DefaultClient.Do(req)
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

// Fetch reads iCal source and returns events for the given date.
func Fetch(ctx context.Context, source string, targetDate string) (*Output, error) {
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", targetDate); err != nil {
		return nil, fmt.Errorf("invalid date format %q: expected YYYY-MM-DD", targetDate)
	}

	if source == "" {
		source = os.Getenv("ICAL_FEED_URL")
	}
	if source == "" {
		return nil, fmt.Errorf("no iCal source specified; provide a source parameter or set ICAL_FEED_URL")
	}

	loc := time.Local
	rc, err := openSource(ctx, source)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	events, err := parseICS(rc, targetDate, loc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse iCal data: %w", err)
	}

	if events == nil {
		events = []Event{}
	}

	return &Output{
		Date:   targetDate,
		Events: events,
	}, nil
}

// RenderJSON marshals the Output struct to JSON (pretty or compact).
func (o *Output) RenderJSON(pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(o, "", "  ")
	}
	return json.Marshal(o)
}
