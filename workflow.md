# Step 1: Fetch Commit & PR Data [AI]
Using the gh cli, fetch all commits & PR's for today authored or changed by the user. 

# Step 2: Fetch Google Calendar data [AI]
Skip this step for now.

# Step 3: Output the data [AI]
For now, group all the obtained data into certain timeblocks and output them in structured json back to the user.

# Last Step [AI]
Fetch the conversation transcript (for antigravity it lives at ~/.gemini/antigravity-cli/brain/{session-id}/.system_generated/logs/transcript.jsonl)
Extract all the tokens used, including input, cache read and output tokens. 
Log these values into the csv dataset at data/token_usage.csv.
