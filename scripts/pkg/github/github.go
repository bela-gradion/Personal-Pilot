// Package github collects the active gh CLI account's PRs and commits for one local day.
//
// Fetch only sees the account gh is logged in as ("@me"). Run it once per gh
// CLI account (gh auth switch --user <login>) and combine the outputs.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	UpdatedAt time.Time `json:"updatedAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

type ghCommit struct {
	SHA    string `json:"sha"`
	URL    string `json:"url"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Repository struct {
		FullName string `json:"fullName"`
	} `json:"repository"`
}

type login struct {
	Login string `json:"login"`
}

type pullDetail struct {
	MergedAt *time.Time `json:"merged_at"`
	MergedBy *login     `json:"merged_by"`
}

type review struct {
	User        *login    `json:"user"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type prCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Author  *login `json:"author"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

type PullRequest struct {
	Number    string `json:"number"`
	Title     string `json:"title"`
	Repo      string `json:"repo"`
	Project   string `json:"project"`
	Status    string `json:"status"` // merged | opened | reviewed | updated | unknown
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"` // local time of the user's own activity that day
}

type Commit struct {
	Repo      string `json:"repo"`
	Project   string `json:"project"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	URL       string `json:"url"`
}

type Output struct {
	Date         string        `json:"date"`
	Account      string        `json:"account"`
	PullRequests []PullRequest `json:"pull_requests"`
	Commits      []Commit      `json:"commits"`
	Errors       []string      `json:"errors,omitempty"`
}

// RepoRule is one repo's entry in projects.json. Other fields there are for the skill.
type RepoRule struct {
	Project string `json:"project"`
	Exclude bool   `json:"exclude"`
}

type Config struct {
	Repos map[string]RepoRule `json:"repos"`
}

// LoadConfig reads projects.json; a missing file means no rules.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}

// project returns the project name for repo and whether it should be reported.
func (c Config) project(repo string) (string, bool) {
	for name, r := range c.Repos {
		if strings.EqualFold(name, repo) {
			if r.Project == "" {
				r.Project = repo[strings.LastIndex(repo, "/")+1:]
			}
			return r.Project, !r.Exclude
		}
	}
	return repo[strings.LastIndex(repo, "/")+1:], true
}

func runGH(ctx context.Context, out any, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("gh %s: parse output: %w", strings.Join(args, " "), err)
	}
	return nil
}

func local(t time.Time) string { return t.In(time.Local).Format(time.RFC3339) }

// summarizePR decides what the user did on a PR inside [start, end).
// It returns false when the user had no own activity on it that day.
func summarizePR(pr ghPR, d pullDetail, reviews []review, commits []prCommit, me string, start, end time.Time) (PullRequest, []prCommit, bool) {
	in := func(t time.Time) bool { return !t.Before(start) && t.Before(end) }
	authored := strings.EqualFold(pr.Author.Login, me)

	var mine []prCommit
	var lastCommit time.Time
	for _, c := range commits {
		if c.Author != nil && strings.EqualFold(c.Author.Login, me) && in(c.Commit.Author.Date) {
			mine = append(mine, c)
			if c.Commit.Author.Date.After(lastCommit) {
				lastCommit = c.Commit.Author.Date
			}
		}
	}
	var lastReview time.Time
	for _, r := range reviews {
		if r.User != nil && strings.EqualFold(r.User.Login, me) && in(r.SubmittedAt) && r.SubmittedAt.After(lastReview) {
			lastReview = r.SubmittedAt
		}
	}

	var status string
	var ts time.Time
	switch {
	case d.MergedAt != nil && in(*d.MergedAt) && (authored || (d.MergedBy != nil && strings.EqualFold(d.MergedBy.Login, me))):
		status, ts = "merged", *d.MergedAt
	case authored && in(pr.CreatedAt):
		status, ts = "opened", pr.CreatedAt
	case !lastReview.IsZero():
		status, ts = "reviewed", lastReview
	case !lastCommit.IsZero():
		status, ts = "updated", lastCommit
	default:
		return PullRequest{}, nil, false
	}
	return PullRequest{
		Number:    fmt.Sprintf("#%d", pr.Number),
		Title:     strings.TrimSpace(pr.Title),
		Repo:      pr.Repository.NameWithOwner,
		Status:    status,
		URL:       pr.URL,
		Timestamp: local(ts),
	}, mine, true
}

// Fetch retrieves the active account's PRs and commits for targetDate (YYYY-MM-DD, local time).
// Failed lookups are listed in Output.Errors; the rest of the data is still returned.
func Fetch(ctx context.Context, targetDate string, cfg Config) (*Output, error) {
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	}
	start, err := time.ParseInLocation("2006-01-02", targetDate, time.Local)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", targetDate)
	}
	end := start.AddDate(0, 0, 1)
	// GitHub search reads plain dates as UTC days; a timestamp range keeps the local day.
	window := start.Format(time.RFC3339) + ".." + end.Add(-time.Second).Format(time.RFC3339)

	var me login
	if err := runGH(ctx, &me, "api", "user"); err != nil {
		return nil, err
	}

	out := &Output{Date: targetDate, Account: me.Login, PullRequests: []PullRequest{}, Commits: []Commit{}}
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		prs     = map[string]ghPR{}
		commits = map[string]Commit{}
	)
	fail := func(err error) { mu.Lock(); out.Errors = append(out.Errors, err.Error()); mu.Unlock() }
	addCommit := func(sha, repo, msg, url string, t time.Time) {
		project, ok := cfg.project(repo)
		if !ok {
			return
		}
		mu.Lock()
		commits[sha] = Commit{Repo: repo, Project: project, Message: strings.TrimSpace(msg), Timestamp: local(t), URL: url}
		mu.Unlock()
	}

	for _, role := range []string{"--author", "--reviewed-by", "--involves"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res []ghPR
			if err := runGH(ctx, &res, "search", "prs", role, "@me", "--updated", window, "--limit", "100",
				"--json", "number,title,state,url,repository,createdAt,updatedAt,author"); err != nil {
				fail(err)
				return
			}
			mu.Lock()
			for _, pr := range res {
				prs[pr.URL] = pr
			}
			mu.Unlock()
		}()
	}
	for _, dateFlag := range []string{"--author-date", "--committer-date"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res []ghCommit
			if err := runGH(ctx, &res, "search", "commits", "--author", "@me", dateFlag, window, "--limit", "100",
				"--json", "commit,repository,sha,url"); err != nil {
				fail(err)
				return
			}
			for _, c := range res {
				addCommit(c.SHA, c.Repository.FullName, c.Commit.Message, c.URL, c.Commit.Author.Date)
			}
		}()
	}
	wg.Wait()

	for _, pr := range prs {
		repo := pr.Repository.NameWithOwner
		project, ok := cfg.project(repo)
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			base := fmt.Sprintf("repos/%s/pulls/%d", repo, pr.Number)
			var (
				d   pullDetail
				rs  []review
				cs  []prCommit
				err error
			)
			// shortcut: first 100 reviews/commits per PR only, page through them if a PR ever exceeds that.
			if err = runGH(ctx, &d, "api", base); err == nil {
				if err = runGH(ctx, &rs, "api", base+"/reviews?per_page=100"); err == nil {
					err = runGH(ctx, &cs, "api", base+"/commits?per_page=100")
				}
			}
			if err != nil {
				fail(err)
				mu.Lock()
				out.PullRequests = append(out.PullRequests, PullRequest{
					Number: fmt.Sprintf("#%d", pr.Number), Title: strings.TrimSpace(pr.Title), Repo: repo,
					Project: project, Status: "unknown", URL: pr.URL, Timestamp: local(pr.UpdatedAt),
				})
				mu.Unlock()
				return
			}
			p, mine, ok := summarizePR(pr, d, rs, cs, me.Login, start, end)
			if !ok {
				return
			}
			p.Project = project
			mu.Lock()
			out.PullRequests = append(out.PullRequests, p)
			mu.Unlock()
			for _, c := range mine {
				addCommit(c.SHA, repo, c.Commit.Message, c.HTMLURL, c.Commit.Author.Date)
			}
		}()
	}
	wg.Wait()

	for _, c := range commits {
		out.Commits = append(out.Commits, c)
	}
	sort.Slice(out.PullRequests, func(i, j int) bool { return out.PullRequests[i].Timestamp < out.PullRequests[j].Timestamp })
	sort.Slice(out.Commits, func(i, j int) bool { return out.Commits[i].Timestamp < out.Commits[j].Timestamp })
	sort.Strings(out.Errors)
	return out, nil
}

// RenderJSON marshals the Output struct to JSON (pretty or compact).
func (o *Output) RenderJSON(pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(o, "", "  ")
	}
	return json.Marshal(o)
}
