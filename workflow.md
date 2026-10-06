# Step 1: Fetch Commit & PR Data [Script]
Use the fetch-github script in ./scripts (just execute it, no args needed, already authenticated) to scrape both PR's and Commits. Dont run anything except the compiled binary for this

# Step 2: Fetch Google Calendar data [AI]
Skip this step for now.

# Step 3: Output the data [AI]
For now, group all the obtained data into certain timeblocks and output them in structured json back to the user.

# Last Step [AI]
Fetch the conversation transcript (for antigravity it lives at ~/.gemini/antigravity-cli/brain/{session-id}/.system_generated/logs/transcript.jsonl)
Extract all the tokens used, including input, cache read and output tokens. 
Log these values into the csv dataset at data/token_usage.csv.
