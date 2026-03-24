"""Jira plugin helper functions.

Pure utility functions with no protocol or I/O dependencies.
"""

import json
import math
import re

_ISSUE_KEY_RE = re.compile(r"^[A-Z][A-Z0-9_]+-\d+$")

_USER_ALIASES = frozenset({"me", "myself", "currentuser"})


def http_error(status, body):
    """Build a compact error dict from an HTTP error response.

    Prevents raw HTML error pages (auth redirects, 500 pages) from
    flooding the LLM context. Extracts the message from JSON errors
    or truncates non-JSON responses.
    """
    result = {"error": f"HTTP {status}"}

    if isinstance(body, dict):
        # Jira JSON error: {"errorMessages": [...], "errors": {...}}
        msgs = body.get("errorMessages") or body.get("errors")
        if msgs:
            result["detail"] = msgs
        else:
            msg = body.get("message") or body.get("error")
            if msg:
                result["detail"] = msg
    elif isinstance(body, str):
        # HTML or plain text — truncate to prevent token waste
        if len(body) > 200:
            result["detail"] = body[:200] + "... (truncated)"
        elif body:
            result["detail"] = body
    return result


def validate_issue_key(key):
    """Validate and return a cleaned issue key.

    Raises ValueError if key doesn't match PROJECT-123 format.
    """
    cleaned = key.strip().upper()
    if not _ISSUE_KEY_RE.match(cleaned):
        raise ValueError(f"Invalid issue key: '{key}' (expected format: PROJECT-123)")
    return cleaned


def is_user_alias(username):
    """Check if username is a self-referencing alias (me, myself, currentUser)."""
    return username.lower().strip() in _USER_ALIASES


def extract_brief_issue(issue):
    """Extract compact summary from a Jira issue response.

    Returns dict with key, summary, status, assignee, priority.
    """
    fields = issue.get("fields", {})
    status = fields.get("status")
    assignee = fields.get("assignee")
    priority = fields.get("priority")
    return {
        "key": issue.get("key", ""),
        "summary": fields.get("summary", ""),
        "status": status.get("name", "") if isinstance(status, dict) else "",
        "assignee": assignee.get("displayName", "") if isinstance(assignee, dict) else "",
        "priority": priority.get("name", "") if isinstance(priority, dict) else "",
    }


def text_to_adf(text):
    """Convert plain text to Atlassian Document Format (ADF).

    Jira Cloud API v3 requires comment/description bodies in ADF.
    Accepts plain text (split into paragraphs), pre-built ADF dicts
    (passed through after validation), or JSON-encoded ADF strings
    (parsed and validated).

    Raises ValueError if a dict is passed that isn't valid ADF.
    """
    # Already a valid ADF dict — pass through
    if isinstance(text, dict):
        if not text:
            pass  # fall through to empty handling below
        elif text.get("type") == "doc" and text.get("version") == 1:
            return text
        else:
            raise ValueError("Invalid ADF dict: must have 'type': 'doc' and 'version': 1")

    if not text:
        return {"version": 1, "type": "doc", "content": [{"type": "paragraph", "content": []}]}

    # Check if the string is a JSON-encoded ADF document
    if isinstance(text, str):
        try:
            parsed = json.loads(text)
            if isinstance(parsed, dict) and parsed.get("type") == "doc" and parsed.get("version") == 1:
                return parsed
        except (json.JSONDecodeError, ValueError):
            pass

    # Plain text — split into paragraphs
    content = []
    for para in str(text).split("\n"):
        if para.strip():
            content.append({"type": "paragraph", "content": [{"type": "text", "text": para}]})
        else:
            content.append({"type": "paragraph", "content": []})

    return {"version": 1, "type": "doc", "content": content}


def adf_to_text(value):
    """Extract plain text from an ADF document, or return value unchanged.

    Jira Cloud v3 returns description and comment bodies as ADF objects.
    This extracts the text content for display. Handles nested content
    nodes recursively.

    Args:
        value: ADF document dict or plain text string

    Returns:
        Plain text string
    """
    if not isinstance(value, dict) or value.get("type") != "doc":
        return value  # Not ADF, return as-is

    parts = []

    def _walk(nodes):
        for node in nodes:
            if node.get("type") == "text":
                parts.append(node.get("text", ""))
            elif "content" in node:
                _walk(node["content"])
            if node.get("type") == "paragraph":
                parts.append("\n")

    _walk(value.get("content", []))
    return "".join(parts).strip()


def normalize_components(components):
    """Normalize a list of component names or dicts to [{name: ...}]."""
    return [c if isinstance(c, dict) else {"name": str(c)} for c in components]


def parse_sprint_field(sprint_data):
    """Parse sprint field from Cloud (dict) or Server (string) format.

    Server format: "com.atlassian...@abc[id=123,name=Sprint 1,state=active,...]"
    Cloud format: {"id": 123, "name": "Sprint 1", "state": "active", ...}
    """
    if isinstance(sprint_data, dict):
        return sprint_data
    if isinstance(sprint_data, str):
        content = sprint_data
        bracket_start = content.find("[")
        bracket_end = content.rfind("]")
        if bracket_start != -1 and bracket_end != -1:
            content = content[bracket_start + 1 : bracket_end]
        info = {}
        for pair in content.split(","):
            if "=" in pair:
                key, value = pair.split("=", 1)
                info[key.strip()] = value.strip()
        return info
    return {}


def natural_sort_key(name):
    """Sort key for numeric-aware ordering of sprint names.

    "Sprint 9" sorts before "Sprint 10" instead of after.
    """
    return [(0, int(c), "") if c.isdigit() else (1, 0, c.lower()) for c in re.split(r"(\d+)", name)]


def escape_jql(value):
    """Escape a value for safe use in double-quoted JQL strings."""
    value = value.replace("\\", "\\\\")
    value = value.replace('"', '\\"')
    value = value.replace("\n", "\\n")
    value = value.replace("\r", "\\r")
    value = value.replace("\0", "")
    return value


def extract_sprint_summary(sprint):
    """Extract compact sprint info: id, name, state, dates."""
    return {
        "id": sprint.get("id"),
        "name": sprint.get("name"),
        "state": sprint.get("state"),
        "startDate": sprint.get("startDate"),
        "endDate": sprint.get("endDate"),
    }


def extract_nested_field(fields_data, field_name):
    """Extract a possibly nested field value from Jira issue fields.

    Handles dotted paths like "status.name", "assignee.displayName".
    For simple names, returns the raw value. For dict values without
    a dotted path, extracts .name or .value if present.
    """
    if "." in field_name:
        parts = field_name.split(".", 1)
        obj = fields_data.get(parts[0])
        if isinstance(obj, dict):
            return obj.get(parts[1])
        return None

    value = fields_data.get(field_name)
    if isinstance(value, dict):
        return value.get("name", value.get("value", str(value)))
    return value


def calculate_sprint_metrics(issues):
    """Calculate basic sprint metrics from an issues list.

    Returns total_issues, completed_issues, completion_rate.
    """
    total = len(issues)
    completed = sum(
        1
        for i in issues
        if (i.get("fields", {}).get("status", {}) or {}).get("statusCategory", {}).get("key") == "done"
    )
    return {
        "total_issues": total,
        "completed_issues": completed,
        "completion_rate": round(completed / total * 100, 1) if total > 0 else 0,
    }


def extract_user_fields(user):
    """Extract standard fields from a Jira user dict."""
    return {
        "accountId": user.get("accountId") or user.get("key"),
        "name": user.get("name"),
        "displayName": user.get("displayName"),
        "emailAddress": user.get("emailAddress"),
        "active": user.get("active"),
        "timeZone": user.get("timeZone"),
    }


def resolve_field_value(value, field_type, is_cloud=False):
    """Convert a value to the appropriate Jira field format.

    Handles type conversion for custom fields based on field_type:
    text, number, select, multi-select, version, user, or auto.

    Returns (converted_value, resolved_field_type).
    """
    if field_type == "auto":
        if isinstance(value, (int, float)):
            field_type = "number"
        elif isinstance(value, list):
            field_type = "multi-select"
        else:
            field_type = "text"

    if field_type == "number":
        try:
            result = float(value)
        except (ValueError, TypeError) as exc:
            raise ValueError(f"Cannot convert {str(value)[:50]!r} to number: {exc}") from exc
        if not math.isfinite(result):
            raise ValueError(f"Number value must be finite, got {result}")
        return result, field_type
    elif field_type == "select":
        return {"value": value}, field_type
    elif field_type == "multi-select":
        values = value if isinstance(value, list) else [value]
        try:
            return [{"value": str(v)} for v in values], field_type
        except (ValueError, TypeError) as exc:
            raise ValueError(f"Invalid value in multi-select list: {exc}") from exc
    elif field_type == "version":
        values = value if isinstance(value, list) else [value]
        try:
            return [{"name": str(v)} for v in values], field_type
        except (ValueError, TypeError) as exc:
            raise ValueError(f"Invalid value in version list: {exc}") from exc
    elif field_type == "user":
        if not isinstance(value, str):
            raise ValueError(f"User field requires a string value (accountId or username), got {type(value).__name__}")
        return ({"accountId": value} if is_cloud else {"name": value}), field_type
    else:
        return value, field_type
