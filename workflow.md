# Step 1: Fetch Commit & PR Data [Script]
Execute the compiled binary `./scripts/fetch-github` in the repository root:
```bash
./scripts/fetch-github
```
- Optionally specify a date with `./scripts/fetch-github -date YYYY-MM-DD` (defaults to today's date) to support backfilling missed days.
- Do not run `go run` or recompile unless explicitly asked.
- The binary searches both authored PRs and PRs reviewed/involved by the authenticated user as well as commits, and outputs structured compact JSON to stdout.

# Step 2: Fetch Google Calendar Data [AI]
Skip this step for now.

# Step 3: Synthesize Timeblocks & Push to Gradion Timesheet [AI]
Synthesize the scraped GitHub activities from Step 1 into distinct chronological timeblocks for the day.

### Timeblock Grouping Guidelines:
- Estimate start and end times based on commit/PR timestamps.
- Group related work into realistic blocks (e.g., 1.5h to 3.0h intervals).
- Output the structured JSON back to the user with the following fields:
  - `date`: Exact date (`YYYY-MM-DD`)
  - `time_window`: Formatted interval (e.g., `09:30 - 12:00`)
  - `duration`: Total duration in hours (e.g., `2.5h`)
  - `project_name`: Repository or project name
  - `classification`: Activity classification name (`Gradion Intern Academy 2026`)
  - `classification_code`: Classification code (`INTGRADI2610`)
  - `task`: Ticket ref / task tag (`#SE` always)
  - `billable`: `false` (default for internal operations)
  - `task_description`: Cohesive description summarizing tasks, PRs, and commits worked on

### Gradion Target: Timesheet App (`log_time`)
When preparing or executing calls to Gradion Workspace:
- **Authentication**: Retrieve token via `security find-generic-password -s GRADION_API_TOKEN -w` (assume it exists; do not echo or write token to files).
- **Target Endpoint**: `POST https://workspace.gradion.com/api/me/apps/timesheet/tools/log_time/call`
- **Required Headers**:
  - `Authorization: Bearer $GRADION_API_TOKEN`
  - `Content-Type: application/json`
- **MCP Call Arguments Schema**:
  ```json
  {
    "arguments": {
      "date": "YYYY-MM-DD",
      "startTime": "HH:MM",
      "endTime": "HH:MM",
      "classification": "INTGRADI2610",
      "task": "#SE",
      "billable": false,
      "description": "<Project>: <Detailed notes about work completed> (PRs: #<pr_number>, ...)"
    }
  }
  ```
- **Execution & Safety Modes**:
  - **Dry-run (Default)**: Unless the user explicitly orders an immediate push in their prompt, treat all runs as dry-runs. Present the structured timeblocks and exact payload arguments to the user first before dispatching any `POST` request.
  - **Live Push**: When confirmed or explicitly ordered to push to timesheet, dispatch the `POST` request via `curl`:
    ```bash
    TOKEN=$(security find-generic-password -s GRADION_API_TOKEN -w)
    curl -s -X POST \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"arguments":{"date":"YYYY-MM-DD","startTime":"HH:MM","endTime":"HH:MM","classification":"INTGRADI2610","task":"#SE","billable":false,"description":"<Description>"}}' \
      https://workspace.gradion.com/api/me/apps/timesheet/tools/log_time/call
    ```
  - **Verification**: Inspect the returned JSON response. Ensure `isError` is `false`. If an error is returned (e.g., overlapping times or invalid range), report the exact error message to the user. Optional: verify existing entries beforehand using `POST https://workspace.gradion.com/api/me/apps/timesheet/tools/list_my_entries/call` with `{"arguments":{"dateFrom":"YYYY-MM-DD","dateTo":"YYYY-MM-DD"}}` to avoid duplicates.

# Step 4: Track Token Usage [Script]
Execute the compiled token tracking binary `./scripts/log-tokens`:
```bash
./scripts/log-tokens -session <session-id>
```
- If `<session-id>` is omitted, the binary automatically inspects the latest session in `~/.gemini/antigravity-cli/brain/`.
- The binary deterministically parses the session transcript (`transcript.jsonl`), extracts `input_tokens`, `output_tokens`, and `cache_read_tokens`, and appends the record to `data/token_usage.csv`.
- Do not run ad-hoc scripts or manual transcript queries.
