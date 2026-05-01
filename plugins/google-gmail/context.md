# Gmail Plugin

## Tool Usage Guidelines

### Write Safety

All write tools default to `dry_run: true`. Always preview first:

1. Call the tool with `dry_run: true` (default)
2. Show the preview to the user
3. Only set `dry_run: false` after explicit approval

This applies to: gmail_send_message, gmail_create_draft,
gmail_modify_labels.

### Reading Email — Recommended Workflow

Always use the tiered approach to avoid token explosion:

1. **List message IDs** with `gmail_list_messages`
2. **Get summaries** with `gmail_get_messages_summary` (lightweight)
3. **Cache if needed** with `gmail_fetch_and_cache` for bulk processing

Never jump straight to `gmail_get_messages` — it returns full
payloads and can consume massive amounts of context. Use
`gmail_get_messages_summary` which returns only essential fields:
id, threadId, date, from, to, subject, snippet, labels.

**Example workflow:**
```
# Step 1: Find message IDs
gmail_list_messages(query="from:alice subject:report", max_results=10)

# Step 2: Get lightweight summaries (use IDs from step 1)
gmail_get_messages_summary(message_ids=["id1", "id2", "id3"])

# Step 3: If you need full content for processing
gmail_fetch_and_cache(query="from:alice subject:report")
# Then read the cache file locally
```

### Search Queries

Gmail search syntax works in the `query` parameter:

**By sender/recipient:**
```
from:alice@example.com
to:me
cc:team@example.com
```

**By content:**
```
subject:quarterly report
has:attachment
filename:pdf
"exact phrase"
```

**By date:**
```
after:2026/01/01
before:2026/03/01
newer_than:7d
older_than:30d
```

**By status/label:**
```
is:unread
is:starred
label:important
in:sent
in:drafts
```

**Combining queries:**
```
from:alice OR from:bob subject:meeting newer_than:7d
```
Spaces between terms act as AND. Use `OR` for alternatives.

### Hard Limits

All tools enforce hard limits to prevent token explosion:

- `gmail_list_messages`: max 100 messages
- `gmail_get_messages_summary`: max 100 messages
- `gmail_get_messages`: max 20 messages (deprecated — use summary)
- `gmail_fetch_and_cache`: max 200 messages

### Caching for Bulk Processing

`gmail_fetch_and_cache` saves full message data to a local cache
directory (`wtmcp/google-gmail/cache/`) and returns only the first
10 summaries. Use this when you need to:

- Process many emails without consuming context tokens
- Extract data from message bodies
- Analyze email patterns over time

After caching, use file operations to read the cached JSON locally.

### Sending Email

**Send a message:**
```
gmail_send_message(
    to="alice@example.com",
    subject="Meeting notes",
    body="Here are the notes from today's meeting...")
```
Always previews first (dry_run default). Supports `cc` and `bcc`.

**Create a draft (doesn't send):**
```
gmail_create_draft(
    to="alice@example.com",
    subject="Draft: proposal",
    body="Draft content here...")
```

### Labels

Use `gmail_list_labels` to discover available label IDs.

**Common label operations:**
```
# Star a message
gmail_modify_labels(message_id="abc123",
                    add_labels=["STARRED"])

# Archive (remove from inbox)
gmail_modify_labels(message_id="abc123",
                    remove_labels=["INBOX"])

# Mark as read
gmail_modify_labels(message_id="abc123",
                    remove_labels=["UNREAD"])

# Move to trash
gmail_modify_labels(message_id="abc123",
                    add_labels=["TRASH"])
```

Common system labels: INBOX, SENT, DRAFT, SPAM, TRASH, UNREAD,
STARRED, IMPORTANT.

### Message IDs

Message IDs come from `gmail_list_messages` or
`gmail_get_messages_summary`. Don't fabricate IDs — always get
them from a previous call.
