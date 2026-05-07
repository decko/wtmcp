# Bugzilla Plugin

## Quick Start

- **Search bugs** → bugzilla_search
- **Get bug details** → bugzilla_get_bugs
- **Read comments** → bugzilla_get_comments
- **File a new bug** → bugzilla_create_bug
- **Update a bug** → bugzilla_update_bug
- **Add a comment** → bugzilla_add_comment

## Investigation Flow

1. Search: `bugzilla_search(query="product:RHEL kernel crash")`
2. Details: `bugzilla_get_bugs(bug_ids="12345")`
3. Comments: `bugzilla_get_comments(bug_id="12345")`
4. History: `bugzilla_get_history(bug_id="12345")`

## Search Patterns

- My bugs: `bugzilla_search(assigned_to="me@redhat.com")`
- Product bugs: `bugzilla_search(product="RHEL", status="NEW")`
- By component: `bugzilla_search(product="RHEL", component="kernel")`
- Recent changes: `bugzilla_search(query="product:RHEL changed:[2w]")`
- Specific bug: `bugzilla_get_bugs(bug_ids="12345")`
- Multiple bugs: `bugzilla_get_bugs(bug_ids="12345,67890,11111")`

## Write Operations

All write tools preview by default (`dry_run=true`). Always follow
this workflow:

1. Call the tool (dry_run is true by default — no need to set it)
2. Show the preview to the user
3. Only set `dry_run=false` after explicit user approval

Common write patterns:

- Close a bug: `bugzilla_update_bug(bug_id="123", status="CLOSED", resolution="FIXED")`
- Reassign: `bugzilla_update_bug(bug_id="123", assigned_to="dev@example.com")`
- Add CC: `bugzilla_update_bug(bug_id="123", cc_add=["manager@example.com"])`
- Mark duplicate: `bugzilla_mark_duplicate(bug_id="123", duplicate_of="456")`

## Status Workflow

Status transitions vary by Bugzilla instance. Example workflow
(Red Hat Bugzilla):

    NEW → ASSIGNED → POST → MODIFIED → ON_QA → VERIFIED → CLOSED

Standard Bugzilla uses fewer statuses (NEW → ASSIGNED → RESOLVED
→ VERIFIED → CLOSED). Always use
`bugzilla_get_fields(field_name="bug_status")` to discover valid
statuses for this instance before updating.

## Resolution Values

Resolutions vary by instance. Example values (Red Hat Bugzilla):
FIXED, WONTFIX, NOTABUG, DUPLICATE, CANTFIX, DEFERRED,
CURRENTRELEASE, NEXTRELEASE, RAWHIDE, ERRATA.

Standard Bugzilla uses: FIXED, INVALID, WONTFIX, DUPLICATE,
WORKSFORME. Use `bugzilla_get_fields(field_name="resolution")`
for the complete list on this instance.

## Brief Mode

Search and get_bugs return compact summaries by default
(`brief=true`). Brief fields: id, summary, status, assigned_to, priority,
severity, product, component, url, resolution (when set).
Set `brief=false` for full bug data including description.

## Aliases

- `bugzilla_whoami` — returns the current authenticated user

## Limitations

- **Authentication**: API key (Bearer token) only; no Kerberos/SSO
- **Attachment downloads** write to the wtmcp output directory
- **Attachment size limit**: 6MB (protocol constraint)
- **Brief mode fields** are fixed (no custom field selection in
  brief mode; use `brief=false` with `include_fields` instead)
- **Date filters** (`new_since`) use UTC; `YYYY-MM-DD` is
  interpreted as midnight UTC
- **Comment `max_results`** is client-side truncation — all
  comments are fetched from the server, then the most recent N
  are returned. Use `new_since` for efficient server-side
  filtering on high-volume bugs
- **Private comments** (`include_private`) may contain embargoed
  security data or customer PII. Filtered client-side by default
  (`false`); Bugzilla server ACLs are the actual security boundary
- **Product and field data** cached for 1 hour; use
  `bugzilla_flush_cache` if values appear stale
