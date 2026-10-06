package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

type PullRequest struct {
	Title      string `json:"title"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner`
	}
	CreatedAt time.Time `json:"createdAt"`
}

type ParsedCommit struct {
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date`
		}
	}
	Repository struct {
		NameWithOwner string `json:"fullName"`
	}
}

func main() {
	date := time.Now().Format("2006-01-02")

	// Fetch Pull Requests
	prCmd := exec.Command("gh", "search", "prs", "--author", "@me", "--updated", date, "--json", "title,repository,createdAt")
	prCmd.Stderr = os.Stderr

	prOut, err := prCmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	var prs []PullRequest
	if err := json.Unmarshal(prOut, &prs); err != nil {
		log.Fatalf("Failed to parse json: %v", err)
	}

	fmt.Printf("Found %d PR(s) for %s:\n", len(prs), date)
	for _, pr := range prs {
		fmt.Println(pr)
	}

	// Fetch Commits
	commitCmd := exec.Command("gh", "search", "commits", "--author", "@me", "--committer-date", date, "--json", "commit,repository")
	commitCmd.Stderr = os.Stderr

	commitOut, err := commitCmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	var commits []ParsedCommit
	if err := json.Unmarshal(commitOut, &commits); err != nil {
		log.Fatalf("Failed to parse json: %v", err)
	}

	fmt.Printf("Found %d Commit(s) for %s:\n", len(commits), date)
	for _, commit := range commits {
		fmt.Println(commit)
	}
}
