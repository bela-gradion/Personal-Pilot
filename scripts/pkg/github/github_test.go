package github

import (
	"testing"
	"time"
)

func TestSummarizePR(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	start := time.Date(2026, 11, 17, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	at := func(h int) time.Time { return start.Add(time.Duration(h) * time.Hour) }
	ptr := func(t time.Time) *time.Time { return &t }

	pr := func(author string, created time.Time) ghPR {
		p := ghPR{Number: 1, CreatedAt: created}
		p.Author.Login = author
		p.Repository.NameWithOwner = "o/r"
		return p
	}
	commit := func(author string, t time.Time) prCommit {
		c := prCommit{SHA: author + t.String(), Author: &login{author}}
		c.Commit.Author.Date = t
		return c
	}
	old := at(-24 * 15)

	cases := []struct {
		name    string
		pr      ghPR
		d       pullDetail
		reviews []review
		commits []prCommit
		status  string
		ts      time.Time
		mine    int
	}{
		{"merged own PR", pr("me", old), pullDetail{MergedAt: ptr(at(15))}, nil, []prCommit{commit("me", at(10))}, "merged", at(15), 1},
		{"merged someone else's PR", pr("x", old), pullDetail{MergedAt: ptr(at(15)), MergedBy: &login{"me"}}, nil, nil, "merged", at(15), 0},
		{"merge by someone else is not mine", pr("x", old), pullDetail{MergedAt: ptr(at(15)), MergedBy: &login{"y"}}, nil, nil, "", time.Time{}, 0},
		{"opened today", pr("me", at(9)), pullDetail{}, nil, nil, "opened", at(9), 0},
		{"old PR with new commits is updated, not opened", pr("me", old), pullDetail{}, nil,
			[]prCommit{commit("me", at(17)), commit("me", at(11)), commit("me", at(-2)), commit("x", at(18))}, "updated", at(17), 2},
		{"review today", pr("x", old), pullDetail{}, []review{{&login{"me"}, at(14)}, {&login{"x"}, at(16)}}, nil, "reviewed", at(14), 0},
		{"review yesterday does not count", pr("x", old), pullDetail{}, []review{{&login{"me"}, at(-1)}}, nil, "", time.Time{}, 0},
		{"only mentioned", pr("x", old), pullDetail{}, nil, nil, "", time.Time{}, 0},
	}
	for _, c := range cases {
		got, mine, ok := summarizePR(c.pr, c.d, c.reviews, c.commits, "me", start, end)
		if ok != (c.status != "") || got.Status != c.status || len(mine) != c.mine {
			t.Errorf("%s: got status %q ok=%v mine=%d, want %q mine=%d", c.name, got.Status, ok, len(mine), c.status, c.mine)
		}
		if ok && got.Timestamp != local(c.ts) {
			t.Errorf("%s: timestamp %s, want %s", c.name, got.Timestamp, local(c.ts))
		}
	}
}

func TestConfigProject(t *testing.T) {
	c := Config{Repos: map[string]RepoRule{
		"o/Named": {Project: "Nice Name"},
		"o/Gone":  {Exclude: true},
	}}
	for _, tc := range []struct {
		repo, project string
		ok            bool
	}{
		{"o/named", "Nice Name", true},
		{"o/Gone", "Gone", false},
		{"o/Other", "Other", true},
	} {
		p, ok := c.project(tc.repo)
		if p != tc.project || ok != tc.ok {
			t.Errorf("%s: got %q %v, want %q %v", tc.repo, p, ok, tc.project, tc.ok)
		}
	}
}
