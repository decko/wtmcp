# GitLab Plugin

## Tool Usage Guidelines

### Instance Configuration

This plugin connects to a single GitLab instance configured via
environment variables:

- `GITLAB_TOKEN` — personal access token (handled by core auth)
- `GITLAB_URL` — base URL (defaults to `https://gitlab.com`)

Authentication is handled by the core HTTP proxy — the plugin
does not access the token directly.

### Project IDs

Most tools require `project_id`. Use the project path:
```
project_id: "group/project"
project_id: "namespace/subgroup/project"
```

Numeric IDs also work but paths are more readable.

### Common Patterns

**My recent commits:**
```
gitlab_get_commits(project_id="team/myproject", max_results=10)
```

**Commits by a specific author:**
```
gitlab_get_commits(project_id="team/myproject", author="alice@example.com")
```

**Commits in a date range:**
```
gitlab_get_commits(project_id="team/myproject",
                   since="2026-03-01", until="2026-03-10")
```

**View a commit's changes:**
```
gitlab_get_commit_diff(project_id="team/myproject",
                       commit_sha="abc123", format="json")
```

### Merge Requests

**My open MRs across all projects:**
```
gitlab_list_merge_requests(scope="created_by_me")
```

**MRs assigned to me:**
```
gitlab_list_merge_requests(scope="assigned_to_me")
```

**MRs in a specific project:**
```
gitlab_list_merge_requests(project_id="team/myproject", state="opened")
```

**Full MR details with comments and diffs:**
```
gitlab_get_merge_request(project_id="team/myproject", mr_iid=42)
```

### CI/CD Pipelines

**Recent pipelines:**
```
gitlab_get_project_pipelines(project_id="team/myproject")
```

**Failed pipelines only:**
```
gitlab_get_project_pipelines(project_id="team/myproject", status="failed")
```

**Pipeline jobs with logs (for debugging failures):**
```
gitlab_get_pipeline_jobs(project_id="team/myproject",
                         pipeline_id=12345, include_logs=true)
```
Job logs are truncated at 50KB.

### Issues

**Open issues in a project:**
```
gitlab_get_project_issues(project_id="team/myproject", state="opened")
```

**Issues assigned to a user:**
```
gitlab_get_project_issues(project_id="team/myproject",
                          assignee="alice", state="opened")
```

**Search issues:**
```
gitlab_get_project_issues(project_id="team/myproject",
                          search="authentication bug")
```

**Full issue details with notes:**
```
gitlab_get_issue_details(project_id="team/myproject", issue_iid=15)
```

### To-Do Items

**Pending to-dos:**
```
gitlab_get_todos()
```

**To-dos for a specific type:**
```
gitlab_get_todos(target_type="MergeRequest")
```

**Pagination — fetch next page:**
```
gitlab_get_todos(page=2)
```

The response includes `page`, `total_pages`, `total_items`, and
`has_next_page` to help navigate results.

The `body` field is truncated to 200 characters for compactness.
For full content, use the detail tools (`gitlab_get_merge_request`,
`gitlab_get_issue_details`).

### IDs and IIDs

- **IID** (Internal ID): project-scoped number shown in the UI
  (e.g., MR !42, Issue #15). Use this in tool parameters.
- **ID**: global numeric ID. Returned in responses but rarely
  needed as input.

### Read-Only Access

This plugin is read-only. It cannot create, modify, or merge
MRs, issues, or any other GitLab resources.
