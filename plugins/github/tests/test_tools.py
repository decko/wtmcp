"""Unit tests for GitHub plugin tools."""

from unittest.mock import patch

import handler
import tools


def _mock_http(status, body, headers=None):
    return patch.object(handler, "http", return_value=(status, body, headers or {}))


def _mock_cache_get(value=None):
    return patch.object(handler, "cache_get", return_value=value)


def _mock_cache_set():
    return patch.object(handler, "cache_set")


SEARCH_ITEMS = [
    {
        "number": 1,
        "title": "Fix bug",
        "state": "open",
        "user": {"login": "alice"},
        "labels": [{"name": "bug"}],
        "updated_at": "2026-03-25T10:00:00Z",
        "html_url": "https://github.com/org/repo/issues/1",
        "repository_url": "https://api.github.com/repos/org/repo",
        "assignees": [{"login": "testuser"}],
    },
    {
        "number": 42,
        "title": "Add feature",
        "state": "open",
        "user": {"login": "testuser"},
        "labels": [],
        "updated_at": "2026-03-24T10:00:00Z",
        "html_url": "https://github.com/org/repo/pull/42",
        "repository_url": "https://api.github.com/repos/org/repo",
        "pull_request": {"url": "..."},
        "assignees": [],
    },
]

SEARCH_RESPONSE = {"total_count": 2, "incomplete_results": False, "items": SEARCH_ITEMS}

ISSUES_RESPONSE = [
    {
        "number": 10,
        "title": "Task A",
        "state": "open",
        "assignee": {"login": "testuser"},
        "labels": [{"name": "task"}],
        "updated_at": "2026-03-25T10:00:00Z",
        "html_url": "https://github.com/org/repo/issues/10",
        "repository": {"full_name": "org/repo"},
    },
]

NOTIFICATIONS_RESPONSE = [
    {
        "id": "1",
        "reason": "review_requested",
        "unread": True,
        "updated_at": "2026-03-25T10:00:00Z",
        "subject": {"type": "PullRequest", "title": "PR Title"},
        "repository": {"full_name": "org/repo"},
    },
]

PR_RESPONSE = {
    "number": 42,
    "title": "Add feature",
    "state": "open",
    "draft": False,
    "body": "Description",
    "user": {"login": "alice"},
    "base": {"ref": "main"},
    "head": {"ref": "feature", "sha": "abc123def456"},
    "merged": False,
    "mergeable": True,
    "mergeable_state": "clean",
    "additions": 10,
    "deletions": 5,
    "changed_files": 3,
    "commits": 2,
    "comments": 1,
    "review_comments": 0,
    "labels": [{"name": "enhancement"}],
    "assignees": [],
    "requested_reviewers": [{"login": "testuser"}],
    "created_at": "2026-03-20T10:00:00Z",
    "updated_at": "2026-03-25T10:00:00Z",
    "merged_at": None,
    "html_url": "https://github.com/org/repo/pull/42",
}

ISSUE_RESPONSE = {
    "number": 10,
    "title": "Task A",
    "state": "open",
    "body": "Do the thing",
    "user": {"login": "alice"},
    "assignees": [{"login": "testuser"}],
    "labels": [{"name": "task"}],
    "milestone": {"title": "v1.0"},
    "comments": 3,
    "created_at": "2026-03-20T10:00:00Z",
    "updated_at": "2026-03-25T10:00:00Z",
    "closed_at": None,
    "html_url": "https://github.com/org/repo/issues/10",
}


# --- github_my_work ---


class TestMyWork:
    def test_basic(self):
        with (
            patch.object(handler, "username", "testuser"),
            _mock_cache_get(None),
            _mock_http(200, SEARCH_RESPONSE),
            _mock_cache_set(),
        ):
            result = tools.my_work({})
            assert result["total"] == 2
            assert result["count"] == 2
            assert result["items"][0]["type"] == "issue"
            assert result["items"][1]["type"] == "pr"

    def test_involvement_author(self):
        with (
            patch.object(handler, "username", "testuser"),
            _mock_cache_get(None),
            _mock_http(200, SEARCH_RESPONSE),
            _mock_cache_set(),
        ):
            result = tools.my_work({})
            assert result["items"][1]["involvement"] == "author"

    def test_involvement_assignee(self):
        with (
            patch.object(handler, "username", "testuser"),
            _mock_cache_get(None),
            _mock_http(200, SEARCH_RESPONSE),
            _mock_cache_set(),
        ):
            result = tools.my_work({})
            assert result["items"][0]["involvement"] == "assignee"

    def test_no_username(self):
        with patch.object(handler, "username", ""):
            result = tools.my_work({})
            assert "error" in result

    def test_org_filter(self):
        with (
            patch.object(handler, "username", "testuser"),
            _mock_cache_get(None),
            _mock_http(200, SEARCH_RESPONSE) as mock_http,
            _mock_cache_set(),
        ):
            tools.my_work({"org": "myorg"})
            query = mock_http.call_args[1].get("query") or mock_http.call_args[0][2]
            assert "org:myorg" in query["q"]

    def test_invalid_org_rejected(self):
        with patch.object(handler, "username", "testuser"):
            try:
                tools.my_work({"org": "my org; drop"})
                assert False, "should have raised"
            except ValueError:
                pass

    def test_cache_hit(self):
        cached = {"total": 1, "count": 1, "items": []}
        with (
            patch.object(handler, "username", "testuser"),
            _mock_cache_get(cached),
        ):
            result = tools.my_work({})
            assert result == cached


# --- github_my_prs_to_review ---


class TestMyPrsToReview:
    def test_basic(self):
        with (
            patch.object(handler, "username", "testuser"),
            _mock_cache_get(None),
            _mock_http(200, SEARCH_RESPONSE),
            _mock_cache_set(),
        ):
            result = tools.my_prs_to_review({})
            assert result["count"] == 2

    def test_query_includes_review_requested(self):
        with (
            patch.object(handler, "username", "testuser"),
            _mock_cache_get(None),
            _mock_http(200, SEARCH_RESPONSE) as mock_http,
            _mock_cache_set(),
        ):
            tools.my_prs_to_review({})
            query = mock_http.call_args[1].get("query") or mock_http.call_args[0][2]
            assert "review-requested:testuser" in query["q"]
            assert "is:pr" in query["q"]


# --- github_my_issues ---


class TestMyIssues:
    def test_basic(self):
        with _mock_cache_get(None), _mock_http(200, ISSUES_RESPONSE), _mock_cache_set():
            result = tools.my_issues({})
            assert result["count"] == 1
            assert result["issues"][0]["number"] == 10

    def test_invalid_filter(self):
        try:
            tools.my_issues({"filter": "invalid"})
            assert False, "should have raised"
        except ValueError:
            pass

    def test_labels_passed(self):
        with _mock_cache_get(None), _mock_http(200, ISSUES_RESPONSE) as mock_http, _mock_cache_set():
            tools.my_issues({"labels": "bug,urgent"})
            query = mock_http.call_args[1].get("query") or mock_http.call_args[0][2]
            assert query["labels"] == "bug,urgent"


# --- github_my_notifications ---


class TestMyNotifications:
    def test_basic(self):
        with _mock_cache_get(None), _mock_http(200, NOTIFICATIONS_RESPONSE), _mock_cache_set():
            result = tools.my_notifications({})
            assert result["count"] == 1
            assert result["notifications"][0]["reason"] == "review_requested"
            assert result["notifications"][0]["subject_type"] == "PullRequest"

    def test_http_error(self):
        with _mock_cache_get(None), _mock_http(401, {"message": "Bad credentials"}):
            result = tools.my_notifications({})
            assert "error" in result


# --- github_get_pr ---


class TestGetPr:
    def test_basic(self):
        with _mock_cache_get(None), _mock_http(200, PR_RESPONSE), _mock_cache_set():
            result = tools.get_pr({"repo": "org/repo", "pr_number": 42})
            assert result["number"] == 42
            assert result["title"] == "Add feature"
            assert result["additions"] == 10
            assert result["head_sha"] == "abc123def456"
            assert result["requested_reviewers"] == ["testuser"]

    def test_invalid_repo(self):
        try:
            tools.get_pr({"repo": "invalid", "pr_number": 1})
            assert False, "should have raised"
        except ValueError:
            pass


# --- github_get_issue ---


class TestGetIssue:
    def test_basic(self):
        with _mock_cache_get(None), _mock_http(200, ISSUE_RESPONSE), _mock_cache_set():
            result = tools.get_issue({"repo": "org/repo", "issue_number": 10})
            assert result["number"] == 10
            assert result["milestone"] == "v1.0"
            assert result["assignees"] == ["testuser"]


# --- github_get_pr_files ---


class TestGetPrFiles:
    def test_basic(self):
        files = [
            {
                "filename": "main.go",
                "status": "modified",
                "additions": 5,
                "deletions": 2,
                "changes": 7,
                "patch": "@@ ...",
            },
        ]
        with _mock_http(200, files):
            result = tools.get_pr_files({"repo": "org/repo", "pr_number": 42})
            assert result["count"] == 1
            assert result["files"][0]["filename"] == "main.go"


# --- github_get_pr_reviews ---


class TestGetPrReviews:
    def test_basic(self):
        reviews = [
            {
                "id": 1,
                "user": {"login": "bob"},
                "state": "APPROVED",
                "body": "LGTM",
                "submitted_at": "2026-03-25T10:00:00Z",
            },
        ]
        with _mock_http(200, reviews):
            result = tools.get_pr_reviews({"repo": "org/repo", "pr_number": 42})
            assert result["count"] == 1
            assert result["reviews"][0]["state"] == "APPROVED"


# --- github_get_comments ---


class TestGetComments:
    def test_basic(self):
        comments = [
            {
                "id": 1,
                "user": {"login": "alice"},
                "body": "Looks good",
                "created_at": "2026-03-25T10:00:00Z",
                "updated_at": "2026-03-25T10:00:00Z",
                "author_association": "MEMBER",
            },
        ]
        with _mock_http(200, comments):
            result = tools.get_comments({"repo": "org/repo", "issue_number": 10})
            assert result["count"] == 1
            assert result["comments"][0]["user"] == "alice"


# --- github_get_pr_commits ---


class TestGetPrCommits:
    def test_basic(self):
        commits = [
            {
                "sha": "abc123def456",
                "commit": {"message": "Fix bug", "author": {"name": "Alice", "date": "2026-03-25T10:00:00Z"}},
                "author": {"login": "alice"},
            },
        ]
        with _mock_http(200, commits):
            result = tools.get_pr_commits({"repo": "org/repo", "pr_number": 42})
            assert result["count"] == 1
            assert result["commits"][0]["sha"] == "abc123def456"[:12]
            assert result["commits"][0]["author"] == "alice"


# --- github_search ---


class TestSearch:
    def test_basic(self):
        with _mock_cache_get(None), _mock_http(200, SEARCH_RESPONSE), _mock_cache_set():
            result = tools.search({"query": "is:issue is:open"})
            assert result["count"] == 2

    def test_empty_query(self):
        result = tools.search({"query": ""})
        assert "error" in result

    def test_pagination(self):
        with _mock_cache_get(None), _mock_http(200, SEARCH_RESPONSE) as mock_http, _mock_cache_set():
            tools.search({"query": "is:pr", "start_at": 50, "max_results": 25})
            query = mock_http.call_args[1].get("query") or mock_http.call_args[0][2]
            assert query["per_page"] == "25"
            assert query["page"] == "3"  # (50 // 25) + 1


# --- Rate limit ---


class TestRateLimit:
    def test_warning_added(self):
        with _mock_cache_get(None), _mock_http(200, PR_RESPONSE, {"X-RateLimit-Remaining": "42"}), _mock_cache_set():
            result = tools.get_pr({"repo": "org/repo", "pr_number": 42})
            assert result["_rate_limit_remaining"] == 42

    def test_no_warning_when_high(self):
        with _mock_cache_get(None), _mock_http(200, PR_RESPONSE, {"X-RateLimit-Remaining": "4999"}), _mock_cache_set():
            result = tools.get_pr({"repo": "org/repo", "pr_number": 42})
            assert "_rate_limit_remaining" not in result
