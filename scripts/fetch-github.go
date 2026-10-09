package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type GHPR struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	State      string    `json:"state"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"createdAt"`
	ClosedAt   time.Time `json:"closedAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Author     struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		Name          string `json:"name"`
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

type GHCommit struct {
	SHA    string `json:"sha"`
	URL    string `json:"url"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Repository struct {
		FullName string `json:"fullName"`
		Name     string `json:"name"`
	} `json:"repository"`
}

type PullRequestOutput struct {
	Number    string `json:"number"`
	Title     string `json:"title"`
	Repo      string `json:"repo"`
	Status    string `json:"status"`
	URL       string `json:"url"`
	Timestamp string `json:"timestamp,omitempty"`
}

type CommitOutput struct {
	Repo      string `json:"repo"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	URL       string `json:"url,omitempty"`
}

type Output struct {
	Date         string              `json:"date"`
	PullRequests []PullRequestOutput `json:"pull_requests"`
	Commits      []CommitOutput      `json:"commits"`
}

func runGH(args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("gh", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func determinePRStatus(pr GHPR, isAuthored bool) string {
	state := strings.ToLower(pr.State)
	if state == "merged" {
		if isAuthored {
			return "merged"
		}
		return "reviewed"
	}
	if !isAuthored {
		return "reviewed"
	}
	if state == "open" {
		return "opened"
	}
	if state == "closed" {
		return "closed"
	}
	return state
}

func getPRTimestamp(pr GHPR, status string) string {
	switch status {
	case "merged":
		if !pr.ClosedAt.IsZero() {
			return pr.ClosedAt.Format(time.RFC3339)
		}
	case "opened":
		if !pr.CreatedAt.IsZero() {
			return pr.CreatedAt.Format(time.RFC3339)
		}
	}
	if !pr.UpdatedAt.IsZero() {
		return pr.UpdatedAt.Format(time.RFC3339)
	}
	if !pr.CreatedAt.IsZero() {
		return pr.CreatedAt.Format(time.RFC3339)
	}
	return ""
}

func main() {
	dateFlag := flag.String("date", "", "Target date in YYYY-MM-DD format (defaults to today)")
	prettyFlag := flag.Bool("pretty", false, "Output pretty-printed JSON (defaults to compact JSON)")
	flag.Parse()

	targetDate := *dateFlag
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	} else {
		if _, err := time.Parse("2006-01-02", targetDate); err != nil {
			log.Fatalf("Invalid date format %q: expected YYYY-MM-DD", targetDate)
		}
	}

	var (
		authoredPRs      []GHPR
		reviewedPRs      []GHPR
		involvedPRs      []GHPR
		committerCommits []GHCommit
		authorCommits    []GHCommit
		errs             []error
		mu               sync.Mutex
	)

	var wg sync.WaitGroup
	wg.Add(5)

	// 1. Authored PRs
	go func() {
		defer wg.Done()
		out, err := runGH("search", "prs", "--author", "@me", "--updated", targetDate, "--limit", "100", "--json", "number,title,state,url,repository,createdAt,closedAt,updatedAt,author")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []GHPR
		if err := json.Unmarshal(out, &res); err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("failed to parse authored PRs: %w", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		authoredPRs = res
		mu.Unlock()
	}()

	// 2. Reviewed PRs
	go func() {
		defer wg.Done()
		out, err := runGH("search", "prs", "--reviewed-by", "@me", "--updated", targetDate, "--limit", "100", "--json", "number,title,state,url,repository,createdAt,closedAt,updatedAt,author")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []GHPR
		if err := json.Unmarshal(out, &res); err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("failed to parse reviewed PRs: %w", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		reviewedPRs = res
		mu.Unlock()
	}()

	// 3. Involved PRs
	go func() {
		defer wg.Done()
		out, err := runGH("search", "prs", "--involves", "@me", "--updated", targetDate, "--limit", "100", "--json", "number,title,state,url,repository,createdAt,closedAt,updatedAt,author")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []GHPR
		if err := json.Unmarshal(out, &res); err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("failed to parse involved PRs: %w", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		involvedPRs = res
		mu.Unlock()
	}()

	// 4. Commits (committer-date)
	go func() {
		defer wg.Done()
		out, err := runGH("search", "commits", "--author", "@me", "--committer-date", targetDate, "--limit", "100", "--json", "commit,repository,sha,url")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []GHCommit
		if err := json.Unmarshal(out, &res); err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("failed to parse committer commits: %w", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		committerCommits = res
		mu.Unlock()
	}()

	// 5. Commits (author-date)
	go func() {
		defer wg.Done()
		out, err := runGH("search", "commits", "--author", "@me", "--author-date", targetDate, "--limit", "100", "--json", "commit,repository,sha,url")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []GHCommit
		if err := json.Unmarshal(out, &res); err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("failed to parse author commits: %w", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		authorCommits = res
		mu.Unlock()
	}()

	wg.Wait()

	if len(errs) > 0 {
		for _, e := range errs {
			log.Printf("Error: %v", e)
		}
		os.Exit(1)
	}

	// Deduplicate PRs
	uniquePRs := make(map[string]GHPR)
	isAuthored := make(map[string]bool)

	for _, pr := range authoredPRs {
		uniquePRs[pr.URL] = pr
		isAuthored[pr.URL] = true
	}
	for _, pr := range reviewedPRs {
		if _, exists := uniquePRs[pr.URL]; !exists {
			uniquePRs[pr.URL] = pr
		}
	}
	for _, pr := range involvedPRs {
		if _, exists := uniquePRs[pr.URL]; !exists {
			uniquePRs[pr.URL] = pr
		}
	}

	prOutputs := make([]PullRequestOutput, 0, len(uniquePRs))
	for _, pr := range uniquePRs {
		repo := pr.Repository.NameWithOwner
		if repo == "" {
			repo = pr.Repository.Name
		}
		status := determinePRStatus(pr, isAuthored[pr.URL])
		prOutputs = append(prOutputs, PullRequestOutput{
			Number:    fmt.Sprintf("#%d", pr.Number),
			Title:     strings.TrimSpace(pr.Title),
			Repo:      repo,
			Status:    status,
			URL:       pr.URL,
			Timestamp: getPRTimestamp(pr, status),
		})
	}

	sort.Slice(prOutputs, func(i, j int) bool {
		if prOutputs[i].Timestamp != prOutputs[j].Timestamp {
			return prOutputs[i].Timestamp < prOutputs[j].Timestamp
		}
		return prOutputs[i].Number < prOutputs[j].Number
	})

	// Deduplicate Commits
	uniqueCommits := make(map[string]GHCommit)
	for _, c := range committerCommits {
		key := c.SHA
		if key == "" {
			key = c.URL
		}
		if key != "" {
			uniqueCommits[key] = c
		}
	}
	for _, c := range authorCommits {
		key := c.SHA
		if key == "" {
			key = c.URL
		}
		if key != "" {
			uniqueCommits[key] = c
		}
	}

	commitOutputs := make([]CommitOutput, 0, len(uniqueCommits))
	for _, c := range uniqueCommits {
		repo := c.Repository.FullName
		if repo == "" {
			repo = c.Repository.Name
		}
		commitOutputs = append(commitOutputs, CommitOutput{
			Repo:      repo,
			Message:   strings.TrimSpace(c.Commit.Message),
			Timestamp: c.Commit.Author.Date,
			URL:       c.URL,
		})
	}

	sort.Slice(commitOutputs, func(i, j int) bool {
		if commitOutputs[i].Timestamp != commitOutputs[j].Timestamp {
			return commitOutputs[i].Timestamp < commitOutputs[j].Timestamp
		}
		return commitOutputs[i].URL < commitOutputs[j].URL
	})

	output := Output{
		Date:         targetDate,
		PullRequests: prOutputs,
		Commits:      commitOutputs,
	}

	var outBytes []byte
	var err error
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
