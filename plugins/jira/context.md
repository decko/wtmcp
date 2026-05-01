# Jira Plugin Context

## Tool Usage Guidelines

### Efficient Querying

- Use JQL to get exactly what's needed in a single call
- `resolution = EMPTY` finds all open tickets regardless of status
- Combine conditions: `assignee = currentUser() AND resolution = EMPTY ORDER BY priority DESC`
- Don't re-query the same data — use cached results from previous calls

### Write Operations

All write tools default to `dry_run=true`. Always preview before
executing writes:

1. Call the tool with `dry_run: true` (default)
2. Show the preview to the user
3. Only set `dry_run: false` after explicit approval

This applies to: create_issue, add_comment, edit_comment,
transition_issue, assign_issue, set_priority, set_labels,
add_labels, remove_labels, set_text_field, set_custom_field,
set_story_points, set_components, add_issue_link, issue_worklog,
add_issues_to_sprint, add_issues_to_backlog, add_attachment,
delete_attachment.

### Assignee Aliases

Use `"me"`, `"myself"`, or `"currentUser"` for self-assignment in
jira_get_user, jira_assign_issue, and JQL queries.

### Search Patterns

**My open tickets:**
```
jql: "assignee = currentUser() AND resolution = EMPTY ORDER BY priority DESC, updated DESC"
```

**Current sprint tickets:**
```
jql: "assignee = currentUser() AND sprint in openSprints() ORDER BY status ASC"
```

**Recent activity:**
```
jql: "assignee = currentUser() AND updated >= -7d ORDER BY updated DESC"
```

**Team sprint:**
```
jql: "sprint = 'Sprint Name' ORDER BY assignee ASC, status ASC"
```

### Finding the User's Sprint

When asked "what sprint am I on?" or similar:

1. **Try open sprints first:**
   ```
   jql: "assignee = currentUser() AND sprint in openSprints() ORDER BY updated DESC"
   ```
   If results, that's the current sprint.

2. **If 0 results, check closed sprints** — the sprint may have
   just ended (common at sprint boundaries):
   ```
   jql: "assignee = currentUser() AND sprint in closedSprints() ORDER BY updated DESC"
   max_results: 5
   ```
   The most recently updated tickets reveal the last sprint.

3. **To find the sprint name**, get the board for the project
   (use the project key from the tickets found in step 2):
   ```
   jira_get_all_agile_boards(project_key="PROJ")
   ```
   Then list sprints for that board:
   ```
   jira_list_available_sprints(board_id=<id>, state="closed", limit=3)
   ```
   The most recently closed sprint is the one the user was in.

4. **Answer the question directly.** If the user asks about their
   sprint, report:
   - The sprint name and state (active/closed)
   - The tickets from that sprint and their status
   - If between sprints, say so — don't pivot to listing all
     open tickets (that's a different question)

5. **Don't rely on the sprint field in search responses.** On
   Jira Server, `fields=sprint` often returns empty even when
   tickets are in sprints. Use `sprint in openSprints()` /
   `closedSprints()` as JQL **filters** — these always work.

6. **Don't list all boards.** `jira_get_all_agile_boards` without
   `project_key` returns dozens of boards. Always filter by
   project.

### Sprint Operations

To find sprint names, use `jira_list_available_sprints` first, then
reference the exact sprint name in `jira_get_sprint_issues` or
`jira_search_by_sprint`.

For sprint filtering with JQL, use `sprint = 'Exact Sprint Name'`
or `sprint in openSprints()`.

### Export and Local Analysis

For large datasets, use the export/cache workflow:

1. `jira_export_sprint_data` — save sprint data to a local file
2. `jira_query_local_sprint_data` — filter locally without API calls
3. `jira_sprint_metrics_summary` — calculate completion metrics
4. `jira_compare_sprints` — compare metrics across sprint files

Use `jira_read_cache_summary` and `jira_get_issue_from_cache` to
read exported data efficiently instead of reading raw JSON files.

### Custom Fields

Use `jira_debug_fields` with a search term to find field IDs:
```
jira_debug_fields(search="sprint")    → find sprint field ID
jira_debug_fields(search="story")     → find story points field
jira_debug_fields(search="team")      → find team field
```

Then use `jira_set_custom_field` with the discovered field ID.

For version-type custom fields, use `field_type: "version"`:
```
jira_set_custom_field(issue_key="PROJ-123",
                      field_id="customfield_12311140",
                      value="rhel-10.2",
                      field_type="version")
```
Use `jira_debug_fields(search="version")` to find the right field ID.

### Cloud vs Server

The plugin auto-detects Cloud vs Server at startup. Differences
handled automatically:

- Comments and descriptions use ADF format on Cloud, plain text on Server
- User assignment uses accountId on Cloud, username on Server
- Sprint report (Greenhopper API) is only available on Server/DC

### Attachments

Use `file_path` to attach files. Relative paths resolve against the
caller's working directory:
```
jira_add_attachment(issue_key="PROJ-123",
                    file_path="path/to/file.png")
```

### File Export and Local Analysis

Export paths are relative to the output directory (`wtmcp/jira/`).
Read paths resolve against both the output and session directories:
```
jira_export_sprint_data(board_id="42", sprint_id="123",
                        output_file="sprint-123.json")
jira_query_local_sprint_data(file_path="sprint-123.json")
```

### Brief Mode

Search and sprint tools return compact summaries by default
(`brief: true`): key, summary, status, assignee, priority.

Set `brief: false` to get clean extracted values for the fields
specified in the `fields` parameter. Dict-valued fields (status,
priority, assignee) are flattened to their display name. ADF
content (description on Cloud) is converted to plain text. Array
fields (labels, components) pass through as lists.

Use the `fields` parameter to control which fields are returned:
```
fields: "summary,status,labels,description,created"
```
