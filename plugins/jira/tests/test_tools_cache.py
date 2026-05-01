"""Unit tests for tools_cache.py — export, local query, and diagnostics."""

import json
import os
from pathlib import Path
from unittest.mock import patch

import handler
import pytest
import tools_cache


@pytest.fixture(autouse=True)
def _mock_invalidate():
    """Mock invalidate_cache for all tests — it uses protocol I/O."""
    with patch.object(handler, "invalidate_cache") as mock:
        yield mock


@pytest.fixture(autouse=True)
def _set_session_dirs(tmp_path):
    """Set _session_dir and _output_dir in handler config for all tests."""
    handler.config["_session_dir"] = str(tmp_path)
    handler.config["_output_dir"] = str(tmp_path)
    yield
    handler.config.pop("_session_dir", None)
    handler.config.pop("_output_dir", None)


def _mock_http(status, body):
    return patch.object(handler, "http", return_value=(status, body, {}))


SAMPLE_SPRINT_EXPORT = {
    "export_metadata": {"tool": "jira_export_sprint_data"},
    "sprint_info": {"id": 1, "name": "Sprint 1", "state": "closed"},
    "issues": [
        {
            "key": "PROJ-1",
            "fields": {
                "summary": "First",
                "status": {"name": "Closed", "statusCategory": {"key": "done"}},
                "assignee": {"displayName": "Alice"},
                "priority": {"name": "High"},
                "issuetype": {"name": "Bug"},
                "labels": ["urgent"],
            },
        },
        {
            "key": "PROJ-2",
            "fields": {
                "summary": "Second",
                "status": {"name": "Open", "statusCategory": {"key": "new"}},
                "assignee": {"displayName": "Bob"},
                "priority": {"name": "Low"},
                "issuetype": {"name": "Story"},
                "labels": [],
            },
        },
    ],
}


def _write_export(tmp_path, data=None, name="sprint.json"):
    """Write test export file and return its path."""
    path = tmp_path / name
    with open(path, "w") as f:
        json.dump(data or SAMPLE_SPRINT_EXPORT, f)
    return str(path)


# --- _validate_export_path ---


class TestValidateExportPath:
    def test_resolves_relative(self, tmp_path):
        path = tools_cache._validate_export_path("out.json")
        assert path.name == "out.json"
        assert str(path).startswith(str(tmp_path))

    def test_resolves_absolute_inside(self, tmp_path):
        path = tools_cache._validate_export_path(str(tmp_path / "out.json"))
        assert path.name == "out.json"

    def test_rejects_outside(self, tmp_path):
        with pytest.raises(ValueError, match="escapes output directory"):
            tools_cache._validate_export_path("/etc/evil.json")

    def test_rejects_traversal(self, tmp_path):
        with pytest.raises(ValueError, match="escapes output directory"):
            tools_cache._validate_export_path("../../etc/passwd")

    def test_rejects_empty(self):
        with pytest.raises(ValueError, match="output_file is required"):
            tools_cache._validate_export_path("")

    def test_rejects_no_output_dir(self):
        handler.config.pop("_output_dir", None)
        with pytest.raises(ValueError, match="configured output directory"):
            tools_cache._validate_export_path("file.json")


# --- _validate_read_path ---


class TestValidateReadPath:
    def test_relative_in_output_dir(self, tmp_path):
        test_file = tmp_path / "data.json"
        test_file.write_text("{}")
        path = tools_cache._validate_read_path("data.json")
        assert path == test_file.resolve()

    def test_relative_in_session_dir(self, tmp_path):
        # Set output_dir to a subdir so session_dir is different
        subdir = tmp_path / "wtmcp" / "jira"
        subdir.mkdir(parents=True)
        handler.config["_output_dir"] = str(subdir)
        test_file = tmp_path / "user_file.md"
        test_file.write_text("hello")
        path = tools_cache._validate_read_path(str(test_file))
        assert path == test_file.resolve()

    def test_absolute_inside_session_dir(self, tmp_path):
        test_file = tmp_path / "report.json"
        test_file.write_text("{}")
        path = tools_cache._validate_read_path(str(test_file))
        assert path == test_file.resolve()

    def test_rejects_outside_both_dirs(self, tmp_path):
        import tempfile

        other = tempfile.mkdtemp()
        other_file = Path(other) / "secret.json"
        other_file.write_text("secret")
        with pytest.raises(FileNotFoundError):
            tools_cache._validate_read_path(str(other_file))

    def test_rejects_traversal(self, tmp_path):
        with pytest.raises(FileNotFoundError):
            tools_cache._validate_read_path("../../etc/passwd")

    def test_rejects_empty(self):
        with pytest.raises(ValueError, match="file_path is required"):
            tools_cache._validate_read_path("")

    def test_rejects_no_dirs_configured(self):
        handler.config.pop("_output_dir", None)
        handler.config.pop("_session_dir", None)
        with pytest.raises(ValueError, match="configured session directory"):
            tools_cache._validate_read_path("file.json")

    def test_nonexistent_file(self, tmp_path):
        with pytest.raises(FileNotFoundError):
            tools_cache._validate_read_path("does_not_exist.json")


# --- jira_export_sprint_data ---


class TestExportSprintData:
    def test_success(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        sprint_info = {"id": 1, "name": "Sprint 1"}
        issues_resp = {"issues": [{"key": "P-1"}]}

        call_count = 0

        def mock_http(_method, path, **_kwargs):
            nonlocal call_count
            call_count += 1
            if "sprint/1" in path and "issue" not in path:
                return 200, sprint_info, {}
            return 200, issues_resp, {}

        with patch.object(handler, "http", side_effect=mock_http):
            result = tools_cache.export_sprint_data(
                {
                    "board_id": "10",
                    "sprint_id": "1",
                    "output_file": str(tmp_path / "out.json"),
                }
            )
            assert result["success"] is True
            assert result["issue_count"] == 1
            # Verify file was created
            assert os.path.exists(str(tmp_path / "out.json"))


# --- jira_query_local_sprint_data ---


class TestQueryLocalSprintData:
    def test_no_filter(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.query_local_sprint_data({"file_path": fp})
        assert result["total_issues"] == 2
        assert result["filtered_count"] == 2

    def test_filter_assignee(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.query_local_sprint_data({"file_path": fp, "assignee": "Alice"})
        assert result["filtered_count"] == 1
        assert result["issues"][0]["key"] == "PROJ-1"

    def test_filter_status(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.query_local_sprint_data({"file_path": fp, "status": "Open"})
        assert result["filtered_count"] == 1

    def test_filter_labels(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.query_local_sprint_data({"file_path": fp, "labels": ["urgent"]})
        assert result["filtered_count"] == 1

    def test_file_not_found(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        with pytest.raises(FileNotFoundError):
            tools_cache.query_local_sprint_data({"file_path": "nope.json"})

    def test_brief_mode(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.query_local_sprint_data({"file_path": fp, "brief": True})
        assert "fields" not in result["issues"][0]
        assert "key" in result["issues"][0]


# --- jira_compare_sprints ---


class TestCompareSpints:
    def test_two_files(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp1 = _write_export(tmp_path, name="s1.json")
        fp2 = _write_export(tmp_path, name="s2.json")
        result = tools_cache.compare_sprints({"file_paths": f"{fp1},{fp2}"})
        assert result["comparison_count"] == 2
        assert result["sprints"][0]["total_issues"] == 2

    def test_missing_file(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        with pytest.raises(FileNotFoundError):
            tools_cache.compare_sprints({"file_paths": "nope.json"})


# --- jira_sprint_metrics_summary ---


class TestSprintMetricsSummary:
    def test_success(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.sprint_metrics_summary({"file_path": fp})
        assert result["total_issues"] == 2
        assert result["completed_issues"] == 1
        assert result["sprint_name"] == "Sprint 1"

    def test_file_not_found(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        with pytest.raises(FileNotFoundError):
            tools_cache.sprint_metrics_summary({"file_path": "nope.json"})


# --- jira_read_cache_summary ---


class TestReadCacheSummary:
    def test_default_fields(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.read_cache_summary({"file_path": fp})
        assert result["total_issues"] == 2
        assert result["returned"] == 2
        assert result["issues"][0]["key"] == "PROJ-1"

    def test_filter_by_keys(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.read_cache_summary({"file_path": fp, "issue_keys": "PROJ-2"})
        assert result["total_issues"] == 1
        assert result["issues"][0]["key"] == "PROJ-2"

    def test_max_issues(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.read_cache_summary({"file_path": fp, "max_issues": 1})
        assert result["returned"] == 1
        assert "note" in result


# --- jira_get_issue_from_cache ---


class TestGetIssueFromCache:
    def test_found(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.get_issue_from_cache({"file_path": fp, "issue_key": "PROJ-1"})
        assert result["key"] == "PROJ-1"
        assert result["summary"] == "First"
        assert result["status"] == "Closed"
        assert result["assignee"] == "Alice"

    def test_not_found(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        fp = _write_export(tmp_path)
        result = tools_cache.get_issue_from_cache({"file_path": fp, "issue_key": "PROJ-999"})
        assert "error" in result


# --- jira_debug_fields ---


class TestDebugFields:
    def test_no_search(self):
        fields = [
            {"id": "summary", "name": "Summary", "schema": {"type": "string"}},
            {"id": "customfield_10020", "name": "Sprint", "schema": {"type": "array"}},
            {"id": "customfield_10028", "name": "Story Points", "schema": {"type": "number"}},
        ]
        with _mock_http(200, fields):
            result = tools_cache.debug_fields({})
            assert result["total_fields"] == 3
            assert result["custom_fields_count"] == 2
            assert len(result["sample_custom_fields"]) == 2

    def test_with_search(self):
        fields = [
            {"id": "customfield_10020", "name": "Sprint", "schema": {"type": "array"}},
            {"id": "customfield_10028", "name": "Story Points", "schema": {"type": "number"}},
        ]
        with _mock_http(200, fields):
            result = tools_cache.debug_fields({"search": "sprint"})
            assert result["match_count"] == 1
            assert result["matching_fields"][0]["name"] == "Sprint"

    def test_http_error(self):
        with _mock_http(500, {"error": "Server Error"}):
            result = tools_cache.debug_fields({})
            assert "error" in result


# --- jira_download_attachment ---


class TestDownloadAttachment:
    def test_text_content(self):
        meta = {
            "filename": "notes.txt",
            "mimeType": "text/plain",
            "size": 11,
            "content": "https://jira.example.com/secure/attachment/123/notes.txt",
        }

        def mock_http(_method, _path, **kwargs):
            if kwargs.get("url"):
                return 200, "hello world", {"Content-Type": "text/plain"}
            return 200, meta, {}

        with patch.object(handler, "http", side_effect=mock_http):
            result = tools_cache.download_attachment({"attachment_id": "123"})
            assert result["filename"] == "notes.txt"
            assert result["encoding"] == "utf-8"
            assert result["content"] == "hello world"

    def test_binary_content(self):
        meta = {
            "filename": "image.png",
            "mimeType": "image/png",
            "size": 4,
            "content": "https://jira.example.com/secure/attachment/456/image.png",
        }

        def mock_http(_method, _path, **kwargs):
            if kwargs.get("url"):
                return 200, b"\x89PNG", {"Content-Type": "image/png"}
            return 200, meta, {}

        with patch.object(handler, "http", side_effect=mock_http):
            result = tools_cache.download_attachment({"attachment_id": "456"})
            assert result["filename"] == "image.png"
            assert result["encoding"] == "base64"
            import base64

            assert base64.b64decode(result["content"]) == b"\x89PNG"

    def test_invalid_id(self):
        import pytest

        with pytest.raises(ValueError, match="Invalid attachment_id"):
            tools_cache.download_attachment({"attachment_id": "abc"})

    def test_no_content_url(self):
        meta = {"filename": "notes.txt", "mimeType": "text/plain", "size": 11}
        with _mock_http(200, meta):
            result = tools_cache.download_attachment({"attachment_id": "123"})
            assert "error" in result
            assert "No content URL" in result["error"]

    def test_metadata_fetch_failure(self):
        with _mock_http(404, {"error": "Not Found"}):
            result = tools_cache.download_attachment({"attachment_id": "999"})
            assert "error" in result

    def test_uses_full_url_from_metadata(self):
        """Verify the content URL from metadata is passed as url= kwarg."""
        content_url = "https://jira.example.com/secure/attachment/789/file.txt"
        meta = {"filename": "file.txt", "mimeType": "text/plain", "size": 5, "content": content_url}

        calls = []

        def mock_http(_method, _path, **kwargs):
            calls.append(kwargs)
            if kwargs.get("url"):
                return 200, "hello", {}
            return 200, meta, {}

        with patch.object(handler, "http", side_effect=mock_http):
            tools_cache.download_attachment({"attachment_id": "789"})
            # Second call should use url= with the content URL
            assert calls[1].get("url") == content_url


# --- jira_add_attachment ---


class TestAddAttachment:
    def test_dry_run(self):
        import base64

        content = base64.b64encode(b"hello").decode()
        result = tools_cache.add_attachment(
            {
                "issue_key": "PROJ-1",
                "filename": "test.txt",
                "content": content,
                "dry_run": True,
            }
        )
        assert result["dry_run"] is True
        assert result["size_bytes"] == 5

    def test_upload_success(self):
        import base64

        content = base64.b64encode(b"data").decode()
        resp = [{"id": "att-1", "filename": "test.txt", "size": 4, "mimeType": "text/plain"}]

        with patch.object(handler, "http_upload", return_value=(200, resp, {})):
            result = tools_cache.add_attachment(
                {
                    "issue_key": "PROJ-1",
                    "filename": "test.txt",
                    "content": content,
                    "dry_run": False,
                }
            )
            assert result["success"] is True
            assert result["id"] == "att-1"

    def test_missing_filename_with_content(self):
        import pytest

        with pytest.raises(ValueError, match="filename is required"):
            tools_cache.add_attachment({"issue_key": "PROJ-1", "content": "abc", "dry_run": False})

    def test_missing_both_file_path_and_content(self):
        import pytest

        with pytest.raises(ValueError, match="either file_path or content"):
            tools_cache.add_attachment({"issue_key": "PROJ-1", "filename": "x.txt", "dry_run": False})

    def test_file_path_dry_run(self, tmp_path):
        test_file = tmp_path / "screenshot.png"
        test_file.write_bytes(b"\x89PNG")
        result = tools_cache.add_attachment(
            {
                "issue_key": "PROJ-1",
                "file_path": str(test_file),
                "dry_run": True,
            }
        )
        assert result["dry_run"] is True
        assert result["filename"] == "screenshot.png"
        assert result["size_bytes"] == 4
        assert result["content_type"] == "image/png"

    def test_file_path_upload(self, tmp_path):
        test_file = tmp_path / "doc.txt"
        test_file.write_text("hello")
        resp = [{"id": "att-2", "filename": "doc.txt", "size": 5, "mimeType": "text/plain"}]

        with patch.object(handler, "http_upload", return_value=(200, resp, {})):
            result = tools_cache.add_attachment(
                {
                    "issue_key": "PROJ-1",
                    "file_path": str(test_file),
                    "dry_run": False,
                }
            )
            assert result["success"] is True
            assert result["id"] == "att-2"

    def test_file_path_not_found(self, tmp_path):
        with pytest.raises(FileNotFoundError):
            tools_cache.add_attachment(
                {
                    "issue_key": "PROJ-1",
                    "file_path": "nonexistent.png",
                    "dry_run": False,
                }
            )


# --- jira_delete_attachment ---


class TestDeleteAttachment:
    def test_dry_run(self):
        result = tools_cache.delete_attachment({"attachment_id": "123", "dry_run": True})
        assert result["dry_run"] is True

    def test_success(self):
        with _mock_http(204, {}):
            result = tools_cache.delete_attachment({"attachment_id": "123", "dry_run": False})
            assert result["success"] is True

    def test_invalid_id(self):
        import pytest

        with pytest.raises(ValueError, match="Invalid attachment_id"):
            tools_cache.delete_attachment({"attachment_id": "abc"})


# --- Cache invalidation ---


class TestAttachmentCacheInvalidation:
    """Verify attachment tools call invalidate_cache on success."""

    def test_add_attachment_invalidates(self, _mock_invalidate):
        import base64

        content = base64.b64encode(b"data").decode()
        resp = [{"id": "att-1", "filename": "f.txt", "size": 4, "mimeType": "text/plain"}]
        with patch.object(handler, "http_upload", return_value=(200, resp, {})):
            tools_cache.add_attachment(
                {"issue_key": "PROJ-1", "filename": "f.txt", "content": content, "dry_run": False}
            )
            _mock_invalidate.assert_called_once_with("PROJ-1")

    def test_add_attachment_dry_run_does_not_invalidate(self, _mock_invalidate):
        import base64

        content = base64.b64encode(b"data").decode()
        tools_cache.add_attachment({"issue_key": "PROJ-1", "filename": "f.txt", "content": content, "dry_run": True})
        _mock_invalidate.assert_not_called()

    def test_delete_attachment_invalidates(self, _mock_invalidate):
        with _mock_http(204, {}):
            tools_cache.delete_attachment({"attachment_id": "123", "dry_run": False})
            _mock_invalidate.assert_called_once_with()

    def test_delete_attachment_dry_run_does_not_invalidate(self, _mock_invalidate):
        tools_cache.delete_attachment({"attachment_id": "123", "dry_run": True})
        _mock_invalidate.assert_not_called()
