package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type ghPR struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	ClosedAt  time.Time `json:"closedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		Name          string `json:"name"`
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

type ghCommit struct {
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

type PullRequest struct {
	Number    string `json:"number"`
	Title     string `json:"title"`
	Repo      string `json:"repo"`
	Status    string `json:"status"`
	URL       string `json:"url"`
	Timestamp string `json:"timestamp,omitempty"`
}

type Commit struct {
	Repo      string `json:"repo"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	URL       string `json:"url,omitempty"`
}

type Output struct {
	Date         string        `json:"date"`
	PullRequests []PullRequest `json:"pull_requests"`
	Commits      []Commit      `json:"commits"`
}

func runGH(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func determinePRStatus(pr ghPR, isAuthored bool) string {
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

func getPRTimestamp(pr ghPR, status string) string {
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

// Fetch retrieves GitHub PRs and commits for the given target date.
func Fetch(ctx context.Context, targetDate string) (*Output, error) {
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", targetDate); err != nil {
		return nil, fmt.Errorf("invalid date format %q: expected YYYY-MM-DD", targetDate)
	}

	var (
		authoredPRs      []ghPR
		reviewedPRs      []ghPR
		involvedPRs      []ghPR
		committerCommits []ghCommit
		authorCommits    []ghCommit
		errs             []error
		mu               sync.Mutex
	)

	var wg sync.WaitGroup
	wg.Add(5)

	// 1. Authored PRs
	go func() {
		defer wg.Done()
		out, err := runGH(ctx, "search", "prs", "--author", "@me", "--updated", targetDate, "--limit", "100", "--json", "number,title,state,url,repository,createdAt,closedAt,updatedAt,author")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []ghPR
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
		out, err := runGH(ctx, "search", "prs", "--reviewed-by", "@me", "--updated", targetDate, "--limit", "100", "--json", "number,title,state,url,repository,createdAt,closedAt,updatedAt,author")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []ghPR
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
		out, err := runGH(ctx, "search", "prs", "--involves", "@me", "--updated", targetDate, "--limit", "100", "--json", "number,title,state,url,repository,createdAt,closedAt,updatedAt,author")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []ghPR
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
		out, err := runGH(ctx, "search", "commits", "--author", "@me", "--committer-date", targetDate, "--limit", "100", "--json", "commit,repository,sha,url")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []ghCommit
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
		out, err := runGH(ctx, "search", "commits", "--author", "@me", "--author-date", targetDate, "--limit", "100", "--json", "commit,repository,sha,url")
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		var res []ghCommit
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
		var msg []string
		for _, e := range errs {
			msg = append(msg, e.Error())
		}
		return nil, fmt.Errorf("github fetch encountered errors:\n - %s", strings.Join(msg, "\n - "))
	}

	// Deduplicate PRs
	uniquePRs := make(map[string]ghPR)
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

	prOutputs := make([]PullRequest, 0, len(uniquePRs))
	for _, pr := range uniquePRs {
		repo := pr.Repository.NameWithOwner
		if repo == "" {
			repo = pr.Repository.Name
		}
		status := determinePRStatus(pr, isAuthored[pr.URL])
		prOutputs = append(prOutputs, PullRequest{
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
	uniqueCommits := make(map[string]ghCommit)
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

	commitOutputs := make([]Commit, 0, len(uniqueCommits))
	for _, c := range uniqueCommits {
		repo := c.Repository.FullName
		if repo == "" {
			repo = c.Repository.Name
		}
		commitOutputs = append(commitOutputs, Commit{
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

	return &Output{
		Date:         targetDate,
		PullRequests: prOutputs,
		Commits:      commitOutputs,
	}, nil
}

// RenderJSON marshals the Output struct to JSON (pretty or compact).
func (o *Output) RenderJSON(pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(o, "", "  ")
	}
	return json.Marshal(o)
}
