---
name: personal-pilot
description: Collects all the work done today by reading Github commit & pr history as well as Google calendar events and combines both into multiple log entries, grouped by topic and time block.
---

# Personal Pilot Skill

Execute the daily procedure below to log work activities:

### General Execution Principles
- **Strict Determinism**: Execute `./scripts/pilot collect-all` (or `~/projects/personal-pilot/scripts/pilot collect-all`) in Step 1.
- **No Extraneous Exploration**: Do **not** run filesystem search commands (`find`, `ls`), `git` commands, or inspect script source code.
- **Default Work Start Time**: Unless specified otherwise, morning work starts at **09:00**.
- **Dry-Run Confirmation Gate**: In dry-run mode, stop after presenting the summary table and wait for explicit user confirmation. Do **NOT** run `log-tokens` (Step 3) until live entries are pushed or the interaction is explicitly concluded.

### Step 1: Fetch Commit, PR & Calendar Data [Script]
Execute the compiled binary `~/projects/personal-pilot/scripts/pilot collect-all`:
```bash
~/projects/personal-pilot/scripts/pilot collect-all
```
- Optionally specify a date with `-date YYYY-MM-DD` (defaults to today's date).
- The binary fetches GitHub commits/PRs and iCal calendar events concurrently in a single step, returning structured JSON containing both `calendar` and `github` data.

### Step 2: Synthesize Timeblocks & Push to Gradion Timesheet [AI]
Synthesize the scraped activities into chronological timeblocks for the day.

1. **Timeblock Grouping Guidelines**:
   - Morning work default start time: `09:00`.
   - Anchor to calendar events first using their `start`/`end` times.
   - Map GitHub commits/PRs that fall within work blocks into the block description.
   - Always prioritize calendar events over GitHub activity.

2. **Gradion Timesheet Target (`log_time`)**:
   - Token: `TOKEN=$(security find-generic-password -s GRADION_API_TOKEN -w)`
   - Endpoint: `POST https://workspace.gradion.com/api/me/apps/timesheet/tools/log_time/call`
   - Payload Arguments Schema:
     ```json
     {
       "arguments": {
         "date": "YYYY-MM-DD",
         "startTime": "HH:MM",
         "endTime": "HH:MM",
         "classification": "INTGRADI2610",
         "task": "#SE",
         "billable": false,
         "description": "<Project>: <Detailed notes> (PRs: #<pr_number>, ...)"
       }
     }
     ```

3. **Execution Mode & Concise Output**:
   - **Dry-run (Default)**: Present a compact markdown summary table of the timeblocks and exact payload arguments to the user. Do not output verbose raw JSON. Wait for confirmation before pushing.
   - **Live Push**: When confirmed or explicitly requested, dispatch the `POST` requests via `curl`:
     ```bash
     TOKEN=$(security find-generic-password -s GRADION_API_TOKEN -w)
     curl -s -X POST \
       -H "Authorization: Bearer $TOKEN" \
       -H "Content-Type: application/json" \
       -d '{"arguments":{"date":"YYYY-MM-DD","startTime":"HH:MM","endTime":"HH:MM","classification":"INTGRADI2610","task":"#SE","billable":false,"description":"<Description>"}}' \
       https://workspace.gradion.com/api/me/apps/timesheet/tools/log_time/call
     ```

### Step 3: Track Token Usage [Script]
Execute the compiled token tracking binary **ONLY AFTER** live timesheet entries have been successfully pushed via `curl` in Step 2 (or if the user explicitly instructs to conclude without pushing live):
```bash
~/projects/personal-pilot/scripts/log-tokens -notes "<descriptive notes>"
```
- Do **NOT** run `log-tokens` during the initial dry-run presentation phase while waiting for user confirmation.
- Summarize token usage in the chat afterwards.

