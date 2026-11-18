---
name: personal-pilot
description: Collects the day's work from GitHub (PRs and commits, per gh account) and the Scylla agenda, proposes timeblocked timesheet entries grouped by topic, and pushes them to the Gradion timesheet after confirmation.
---

# Personal Pilot Skill

Log one day of work as timeblocked timesheet entries.

### General Execution Principles
- **Date**: today unless the user names a day. All times are local.
- **Rules live in `~/projects/personal-pilot/projects.json`** (local only, gitignored; copy `projects.example.json` to start): gh accounts, project names, excluded repos, priorities, max blocks per project, and timesheet defaults. Read it once per run. The user's instructions in the chat override it.
- **Cross-check freely**: if the user says work is missing, query GitHub directly (`gh api repos/<repo>/commits?sha=<branch>&since=...&author=<login>`) for the repos they name.
- **Dry-run gate**: always propose first. Push only after the user confirms.

### Step 1: GitHub data [Script]
The collector only sees the active gh account, so run it once per account listed in `accounts` in `projects.json`, then switch back to the first one:
```bash
gh auth switch --user <account>
go -C ~/projects/personal-pilot run ./scripts/cmd/pilot github [-date YYYY-MM-DD]
```
- Output per account: `pull_requests` (status `merged`/`opened`/`reviewed`/`updated`, timestamp = the user's own activity that day), `commits` (local timestamps, includes the user's commits on PR branches), each tagged with `project`. Excluded repos are already filtered out.
- `errors` lists lookups that failed; the rest of the data is still valid. Mention any errors in the proposal.
- Known gap: commits on a non-default branch without a PR are not found. Cover it in the same call by listing the user's local commits across `~/projects`, which also finds work that was never pushed (commits that are also in the GitHub output count once):
  ```bash
  for d in ~/projects/*/; do git -C "$d" log --all --since="YYYY-MM-DD 00:00" --until="YYYY-MM-DD 23:59" --author="$(git config user.name)" --format="$(basename "$d") %ad %h %s" --date=format:%H:%M 2>/dev/null; done
  ```
  Ask the user if the day still looks thin.

### Step 2: Agenda [AI]
Get the day's agenda (meetings, events) from the **Scylla MCP** (`get_my_agenda` with `date`).
- The result is too large to return (an HTML widget), so it is saved to a file. Do not probe the file; read only the events in one call:
  ```bash
  jq -r '.[1].text | fromjson | .events[] | [.start, .end, .title] | @tsv' <saved file>
  ```
- Times are UTC. Convert them to local time (the user is on +07:00).
- If the Scylla MCP is not connected, say so, and continue with GitHub data only unless the user pastes the agenda.

### Step 3: Propose timeblocks [AI]
1. Start at `defaults.dayStart`, end at `defaults.dayEnd`, leave `defaults.lunch` free.
2. Anchor agenda events at their real times first; they take priority over GitHub activity.
3. Fill the rest with GitHub work, grouped by topic per project:
   - `priority: "high"` projects get most of the remaining time.
   - Respect `maxBlocks` per project; give tiny unrelated items (e.g. a single merge in another project) their own short block rather than folding them into another project's block.
4. Description format: `<Project>: <grouped topics> (PRs: #1, #2)`. Fields from `defaults`: `classification`, `task`, `billable`.
5. **Mark inferred times**: a timestamp says when work was pushed, merged or reviewed, not when it happened (e.g. many commits pushed in the same minute). Say which blocks rest on real times and which are guesses.
6. Present a compact table (time, description) plus the shared field values. No raw JSON.

### Step 4: Check for existing entries, then push [Script]
Only after the user confirms.
1. List what is already logged for the date, so a rerun never double-logs:
   ```bash
   TOKEN=$(security find-generic-password -s GRADION_API_TOKEN -w)
   curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
     -d '{"arguments":{"dateFrom":"YYYY-MM-DD","dateTo":"YYYY-MM-DD","limit":50}}' \
     https://workspace.gradion.com/api/me/apps/timesheet/tools/list_my_entries/call
   ```
   If entries overlap the proposed blocks, stop and ask the user (skip, or edit via `edit_time`).
2. Push all blocks in one shell call (one `curl` per block):
   ```bash
   curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
     -d '{"arguments":{"date":"YYYY-MM-DD","startTime":"HH:MM","endTime":"HH:MM","classification":"<defaults.classification>","task":"<defaults.task>","billable":<defaults.billable>,"description":"<Description>"}}' \
     https://workspace.gradion.com/api/me/apps/timesheet/tools/log_time/call
   ```
3. Report each result (`isError`, logged time, entry deeplink).

### Step 5: Track token usage [Script]
Only after a live push, or when the user ends the run without pushing, and only if the user has not said to skip it:
```bash
go -C ~/projects/personal-pilot run ./scripts/cmd/pilot log-tokens -notes "<descriptive notes>"
```
It always writes to `data/token_usage.csv` in the repo, whatever the working directory. It logs the current Claude Code session (`$CLAUDE_CODE_SESSION_ID`), so other sessions running at the same time are not counted; pass `-session <id>` to log a different one.
