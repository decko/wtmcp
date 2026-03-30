"""Unit tests for helpers.py — pure functions, no mocking needed."""

import json

import pytest
from helpers import (
    _looks_like_wiki_markup,
    _parse_inline_markup,
    adf_to_text,
    calculate_sprint_metrics,
    escape_jql,
    extract_brief_issue,
    extract_nested_field,
    extract_sprint_summary,
    extract_user_fields,
    is_user_alias,
    natural_sort_key,
    normalize_components,
    parse_sprint_field,
    resolve_field_value,
    text_to_adf,
    validate_issue_key,
    wiki_to_adf,
)

# --- validate_issue_key ---


class TestValidateIssueKey:
    def test_valid_simple(self):
        assert validate_issue_key("PROJ-123") == "PROJ-123"

    def test_valid_uppercase(self):
        assert validate_issue_key("proj-1") == "PROJ-1"

    def test_valid_with_whitespace(self):
        assert validate_issue_key("  ABC-42  ") == "ABC-42"

    def test_valid_with_numbers_in_project(self):
        assert validate_issue_key("RHEL9-100") == "RHEL9-100"

    def test_valid_with_underscore(self):
        assert validate_issue_key("MY_PROJ-7") == "MY_PROJ-7"

    def test_invalid_no_dash(self):
        with pytest.raises(ValueError, match="Invalid issue key"):
            validate_issue_key("PROJ123")

    def test_invalid_no_number(self):
        with pytest.raises(ValueError, match="Invalid issue key"):
            validate_issue_key("PROJ-")

    def test_invalid_starts_with_number(self):
        with pytest.raises(ValueError, match="Invalid issue key"):
            validate_issue_key("123-ABC")

    def test_invalid_empty(self):
        with pytest.raises(ValueError, match="Invalid issue key"):
            validate_issue_key("")

    def test_invalid_just_whitespace(self):
        with pytest.raises(ValueError, match="Invalid issue key"):
            validate_issue_key("   ")

    def test_invalid_lowercase_dash(self):
        # Single lowercase letter doesn't match [A-Z]
        with pytest.raises(ValueError, match="Invalid issue key"):
            validate_issue_key("a-1")


# --- is_user_alias ---


class TestIsUserAlias:
    def test_me(self):
        assert is_user_alias("me") is True

    def test_myself(self):
        assert is_user_alias("myself") is True

    def test_currentuser(self):
        assert is_user_alias("currentUser") is True

    def test_case_insensitive(self):
        assert is_user_alias("ME") is True
        assert is_user_alias("Myself") is True
        assert is_user_alias("CURRENTUSER") is True

    def test_with_whitespace(self):
        assert is_user_alias("  me  ") is True

    def test_regular_username(self):
        assert is_user_alias("jdoe") is False

    def test_empty(self):
        assert is_user_alias("") is False


# --- extract_brief_issue ---


def _make_issue(key="TEST-1", summary="Fix bug", status="Open", assignee="Jane", priority="High"):
    """Build a Jira issue dict for testing."""
    fields = {"summary": summary}
    if status is not None:
        fields["status"] = {"name": status}
    if assignee is not None:
        fields["assignee"] = {"displayName": assignee}
    if priority is not None:
        fields["priority"] = {"name": priority}
    return {"key": key, "fields": fields}


class TestExtractBriefIssue:
    def test_full_fields(self):
        result = extract_brief_issue(_make_issue())
        assert result == {
            "key": "TEST-1",
            "summary": "Fix bug",
            "status": "Open",
            "assignee": "Jane",
            "priority": "High",
        }

    def test_missing_status(self):
        result = extract_brief_issue(_make_issue(status=None))
        assert result["status"] == ""

    def test_missing_assignee(self):
        result = extract_brief_issue(_make_issue(assignee=None))
        assert result["assignee"] == ""

    def test_missing_priority(self):
        result = extract_brief_issue(_make_issue(priority=None))
        assert result["priority"] == ""

    def test_empty_fields(self):
        result = extract_brief_issue({"key": "X-1", "fields": {}})
        assert result["summary"] == ""
        assert result["status"] == ""
        assert result["assignee"] == ""
        assert result["priority"] == ""

    def test_no_fields_key(self):
        result = extract_brief_issue({"key": "X-1"})
        assert result["key"] == "X-1"
        assert result["summary"] == ""

    def test_non_dict_status(self):
        # Edge case: status is a string (shouldn't happen, but be safe)
        issue = {"key": "X-1", "fields": {"status": "Open"}}
        result = extract_brief_issue(issue)
        assert result["status"] == ""


# --- extract_user_fields ---


class TestExtractUserFields:
    def test_cloud_user(self):
        user = {
            "accountId": "abc123",
            "name": "jdoe",
            "displayName": "Jane Doe",
            "emailAddress": "jane@example.com",
            "active": True,
            "timeZone": "America/New_York",
        }
        result = extract_user_fields(user)
        assert result["accountId"] == "abc123"
        assert result["displayName"] == "Jane Doe"
        assert result["emailAddress"] == "jane@example.com"
        assert result["active"] is True
        assert result["timeZone"] == "America/New_York"

    def test_server_user_key_fallback(self):
        user = {
            "key": "jdoe",
            "name": "jdoe",
            "displayName": "Jane Doe",
        }
        result = extract_user_fields(user)
        assert result["accountId"] == "jdoe"

    def test_missing_fields(self):
        result = extract_user_fields({})
        assert result["accountId"] is None
        assert result["displayName"] is None
        assert result["active"] is None


# --- text_to_adf ---


class TestTextToAdf:
    def test_simple_text(self):
        result = text_to_adf("Hello world")
        assert result["version"] == 1
        assert result["type"] == "doc"
        assert len(result["content"]) == 1
        assert result["content"][0]["content"][0]["text"] == "Hello world"

    def test_multiline(self):
        result = text_to_adf("Line 1\nLine 2\nLine 3")
        assert len(result["content"]) == 3
        assert result["content"][0]["content"][0]["text"] == "Line 1"
        assert result["content"][2]["content"][0]["text"] == "Line 3"

    def test_empty_lines_become_empty_paragraphs(self):
        result = text_to_adf("Before\n\nAfter")
        assert len(result["content"]) == 3
        assert result["content"][1]["content"] == []

    def test_empty_string(self):
        result = text_to_adf("")
        assert result["content"] == [{"type": "paragraph", "content": []}]

    def test_none(self):
        result = text_to_adf(None)
        assert result["content"] == [{"type": "paragraph", "content": []}]

    def test_adf_dict_passthrough(self):
        adf = {
            "version": 1,
            "type": "doc",
            "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Pre-built"}]}],
        }
        result = text_to_adf(adf)
        assert result is adf

    def test_adf_json_string_passthrough(self):
        adf = {
            "version": 1,
            "type": "doc",
            "content": [{"type": "paragraph", "content": [{"type": "text", "text": "From JSON"}]}],
        }
        result = text_to_adf(json.dumps(adf))
        assert result == adf

    def test_invalid_adf_dict_raises(self):
        with pytest.raises(ValueError, match="Invalid ADF dict"):
            text_to_adf({"type": "not_doc", "version": 1})

    def test_empty_dict_treated_as_empty(self):
        result = text_to_adf({})
        assert result["content"] == [{"type": "paragraph", "content": []}]

    def test_invalid_adf_dict_wrong_version(self):
        with pytest.raises(ValueError, match="Invalid ADF dict"):
            text_to_adf({"type": "doc", "version": 2})

    def test_json_array_string_treated_as_text(self):
        result = text_to_adf("[1, 2, 3]")
        assert result["type"] == "doc"
        assert result["content"][0]["content"][0]["text"] == "[1, 2, 3]"

    def test_json_string_invalid_adf_treated_as_text(self):
        result = text_to_adf('{"type": "table", "version": 1}')
        assert result["type"] == "doc"
        assert result["content"][0]["content"][0]["text"] == '{"type": "table", "version": 1}'

    def test_non_adf_json_string_treated_as_text(self):
        result = text_to_adf('{"key": "value"}')
        assert result["type"] == "doc"
        assert result["content"][0]["content"][0]["text"] == '{"key": "value"}'


# --- normalize_components ---


class TestNormalizeComponents:
    def test_string_list(self):
        result = normalize_components(["Web", "API"])
        assert result == [{"name": "Web"}, {"name": "API"}]

    def test_dict_passthrough(self):
        result = normalize_components([{"name": "Web", "id": "123"}])
        assert result == [{"name": "Web", "id": "123"}]

    def test_mixed(self):
        result = normalize_components(["Web", {"name": "API"}])
        assert result == [{"name": "Web"}, {"name": "API"}]

    def test_empty(self):
        assert normalize_components([]) == []


# --- parse_sprint_field ---


class TestParseSprintField:
    def test_cloud_dict(self):
        data = {"id": 123, "name": "Sprint 1", "state": "active"}
        assert parse_sprint_field(data) == data

    def test_server_string(self):
        data = "com.atlassian.greenhopper.service.sprint.Sprint@abc[id=123,name=Sprint 1,state=active]"
        result = parse_sprint_field(data)
        assert result["id"] == "123"
        assert result["name"] == "Sprint 1"
        assert result["state"] == "active"

    def test_empty(self):
        assert parse_sprint_field(None) == {}
        assert parse_sprint_field(42) == {}


# --- natural_sort_key ---


class TestNaturalSortKey:
    def test_numeric_sorting(self):
        names = ["Sprint 10", "Sprint 2", "Sprint 1", "Sprint 9"]
        sorted_names = sorted(names, key=natural_sort_key)
        assert sorted_names == ["Sprint 1", "Sprint 2", "Sprint 9", "Sprint 10"]

    def test_alpha_sorting(self):
        names = ["Beta", "Alpha", "Gamma"]
        assert sorted(names, key=natural_sort_key) == ["Alpha", "Beta", "Gamma"]


# --- escape_jql ---


class TestEscapeJql:
    def test_quotes(self):
        assert escape_jql('Sprint "1"') == 'Sprint \\"1\\"'

    def test_backslash(self):
        assert escape_jql("a\\b") == "a\\\\b"

    def test_newlines(self):
        assert escape_jql("a\nb") == "a\\nb"

    def test_null_bytes(self):
        assert escape_jql("a\0b") == "ab"

    def test_clean_string(self):
        assert escape_jql("Sprint 35") == "Sprint 35"


# --- extract_sprint_summary ---


class TestExtractSprintSummary:
    def test_full(self):
        sprint = {
            "id": 1,
            "name": "S1",
            "state": "active",
            "startDate": "2026-01-01",
            "endDate": "2026-01-14",
            "extra": "x",
        }
        result = extract_sprint_summary(sprint)
        assert result == {"id": 1, "name": "S1", "state": "active", "startDate": "2026-01-01", "endDate": "2026-01-14"}
        assert "extra" not in result

    def test_partial(self):
        result = extract_sprint_summary({"id": 1, "name": "S1"})
        assert result["state"] is None


# --- extract_nested_field ---


class TestExtractNestedField:
    def test_simple_string(self):
        assert extract_nested_field({"summary": "Hello"}, "summary") == "Hello"

    def test_dotted_path(self):
        assert extract_nested_field({"status": {"name": "Open"}}, "status.name") == "Open"

    def test_dotted_missing(self):
        assert extract_nested_field({"status": {"id": 1}}, "status.name") is None

    def test_dict_auto_extract_name(self):
        assert extract_nested_field({"priority": {"name": "High"}}, "priority") == "High"

    def test_dict_auto_extract_value(self):
        assert extract_nested_field({"team": {"value": "Alpha"}}, "team") == "Alpha"

    def test_missing_field(self):
        assert extract_nested_field({}, "nope") is None

    def test_non_dict_parent(self):
        assert extract_nested_field({"status": "Open"}, "status.name") is None


# --- calculate_sprint_metrics ---


class TestCalculateSprintMetrics:
    def test_basic_metrics(self):
        issues = [
            {"fields": {"status": {"statusCategory": {"key": "done"}}}},
            {"fields": {"status": {"statusCategory": {"key": "done"}}}},
            {"fields": {"status": {"statusCategory": {"key": "indeterminate"}}}},
        ]
        result = calculate_sprint_metrics(issues)
        assert result["total_issues"] == 3
        assert result["completed_issues"] == 2
        assert result["completion_rate"] == 66.7

    def test_empty(self):
        result = calculate_sprint_metrics([])
        assert result["total_issues"] == 0
        assert result["completion_rate"] == 0

    def test_all_done(self):
        issues = [{"fields": {"status": {"statusCategory": {"key": "done"}}}}]
        result = calculate_sprint_metrics(issues)
        assert result["completion_rate"] == 100.0

    def test_missing_status(self):
        issues = [{"fields": {}}]
        result = calculate_sprint_metrics(issues)
        assert result["completed_issues"] == 0


# --- resolve_field_value ---


class TestResolveFieldValue:
    # --- auto detection ---
    def test_auto_int(self):
        val, ft = resolve_field_value(42, "auto")
        assert val == 42.0
        assert ft == "number"

    def test_auto_float(self):
        val, ft = resolve_field_value(3.14, "auto")
        assert val == 3.14
        assert ft == "number"

    def test_auto_bool_is_number(self):
        # bool is subclass of int in Python — auto-detects as number
        val, ft = resolve_field_value(True, "auto")
        assert val == 1.0
        assert ft == "number"

    def test_auto_list(self):
        val, ft = resolve_field_value(["a", "b"], "auto")
        assert val == [{"value": "a"}, {"value": "b"}]
        assert ft == "multi-select"

    def test_auto_string(self):
        val, ft = resolve_field_value("hello", "auto")
        assert val == "hello"
        assert ft == "text"

    def test_auto_dict(self):
        val, ft = resolve_field_value({"key": "val"}, "auto")
        assert val == {"key": "val"}
        assert ft == "text"

    # --- explicit types ---
    def test_number_from_string(self):
        val, ft = resolve_field_value("3.14", "number")
        assert val == 3.14

    def test_number_invalid(self):
        with pytest.raises(ValueError, match="Cannot convert"):
            resolve_field_value("abc", "number")

    def test_number_inf(self):
        with pytest.raises(ValueError, match="finite"):
            resolve_field_value(float("inf"), "number")

    def test_number_nan(self):
        with pytest.raises(ValueError, match="finite"):
            resolve_field_value(float("nan"), "number")

    def test_select(self):
        val, ft = resolve_field_value("Option A", "select")
        assert val == {"value": "Option A"}
        assert ft == "select"

    def test_multi_select_single(self):
        val, ft = resolve_field_value("one", "multi-select")
        assert val == [{"value": "one"}]

    def test_multi_select_list(self):
        val, ft = resolve_field_value(["a", "b"], "multi-select")
        assert val == [{"value": "a"}, {"value": "b"}]

    def test_version_single(self):
        val, ft = resolve_field_value("1.0", "version")
        assert val == [{"name": "1.0"}]

    def test_version_list(self):
        val, ft = resolve_field_value(["1.0", "2.0"], "version")
        assert val == [{"name": "1.0"}, {"name": "2.0"}]

    def test_user_cloud(self):
        val, ft = resolve_field_value("abc123", "user", is_cloud=True)
        assert val == {"accountId": "abc123"}

    def test_user_server(self):
        val, ft = resolve_field_value("jdoe", "user", is_cloud=False)
        assert val == {"name": "jdoe"}

    def test_user_invalid_type(self):
        with pytest.raises(ValueError, match="string value"):
            resolve_field_value(123, "user")

    def test_user_empty_string(self):
        val, ft = resolve_field_value("", "user", is_cloud=True)
        assert val == {"accountId": ""}

    def test_text_passthrough(self):
        val, ft = resolve_field_value("hello", "text")
        assert val == "hello"
        assert ft == "text"

    def test_unknown_type_passthrough(self):
        val, ft = resolve_field_value({"custom": True}, "unknown")
        assert val == {"custom": True}
        assert ft == "unknown"


class TestAdfToText:
    """Test adf_to_text() ADF-to-plain-text conversion."""

    def test_plain_string_passthrough(self):
        """Non-ADF strings pass through unchanged."""
        assert adf_to_text("Hello world") == "Hello world"
        assert adf_to_text("") == ""

    def test_none_passthrough(self):
        """None and non-dict values pass through."""
        assert adf_to_text(None) is None
        assert adf_to_text(123) == 123
        assert adf_to_text([1, 2, 3]) == [1, 2, 3]

    def test_single_paragraph(self):
        """Simple ADF with one paragraph."""
        adf = {
            "type": "doc",
            "version": 1,
            "content": [
                {
                    "type": "paragraph",
                    "content": [{"type": "text", "text": "Hello world"}],
                }
            ],
        }
        assert adf_to_text(adf) == "Hello world"

    def test_multiple_paragraphs(self):
        """ADF with multiple paragraphs separated by newlines."""
        adf = {
            "type": "doc",
            "version": 1,
            "content": [
                {"type": "paragraph", "content": [{"type": "text", "text": "First paragraph"}]},
                {"type": "paragraph", "content": [{"type": "text", "text": "Second paragraph"}]},
            ],
        }
        assert adf_to_text(adf) == "First paragraph\nSecond paragraph"

    def test_empty_paragraphs(self):
        """Empty paragraphs are handled correctly."""
        adf = {
            "type": "doc",
            "version": 1,
            "content": [
                {"type": "paragraph", "content": [{"type": "text", "text": "Before"}]},
                {"type": "paragraph", "content": []},  # Empty paragraph
                {"type": "paragraph", "content": [{"type": "text", "text": "After"}]},
            ],
        }
        result = adf_to_text(adf)
        assert "Before" in result
        assert "After" in result

    def test_nested_content_bold_and_links(self):
        """Nested content (bold, links) - extract text without markup."""
        adf = {
            "type": "doc",
            "version": 1,
            "content": [
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "Normal "},
                        {"type": "text", "text": "bold", "marks": [{"type": "strong"}]},
                        {"type": "text", "text": " and "},
                        {
                            "type": "text",
                            "text": "link",
                            "marks": [{"type": "link", "attrs": {"href": "http://example.com"}}],
                        },
                    ],
                }
            ],
        }
        assert adf_to_text(adf) == "Normal bold and link"

    def test_complex_nested_structure(self):
        """Complex ADF with lists, headings, etc."""
        adf = {
            "type": "doc",
            "version": 1,
            "content": [
                {"type": "heading", "content": [{"type": "text", "text": "Title"}]},
                {"type": "paragraph", "content": [{"type": "text", "text": "Paragraph"}]},
                {
                    "type": "bulletList",
                    "content": [
                        {
                            "type": "listItem",
                            "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Item 1"}]}],
                        },
                        {
                            "type": "listItem",
                            "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Item 2"}]}],
                        },
                    ],
                },
            ],
        }
        result = adf_to_text(adf)
        assert "Title" in result
        assert "Paragraph" in result
        assert "Item 1" in result
        assert "Item 2" in result

    def test_non_doc_dict_passthrough(self):
        """Dict without type=doc passes through unchanged."""
        not_adf = {"key": "value", "foo": "bar"}
        assert adf_to_text(not_adf) == not_adf


# --- Wiki markup detection ---


class TestLooksLikeWikiMarkup:
    def test_heading(self):
        assert _looks_like_wiki_markup("h2. Title")

    def test_heading_h1(self):
        assert _looks_like_wiki_markup("h1. Main Title")

    def test_heading_h6(self):
        assert _looks_like_wiki_markup("h6. Deep heading")

    def test_unordered_list(self):
        assert _looks_like_wiki_markup("* item one")

    def test_ordered_list(self):
        assert _looks_like_wiki_markup("# first item")

    def test_code_block(self):
        assert _looks_like_wiki_markup("{code}\nprint('hi')\n{code}")

    def test_code_block_with_lang(self):
        assert _looks_like_wiki_markup("{code:python}\nx = 1\n{code}")

    def test_quote_block(self):
        assert _looks_like_wiki_markup("{quote}\nsome quote\n{quote}")

    def test_bq_shorthand(self):
        assert _looks_like_wiki_markup("bq. Some quoted text")

    def test_panel(self):
        assert _looks_like_wiki_markup("{panel}\ncontent\n{panel}")

    def test_horizontal_rule(self):
        assert _looks_like_wiki_markup("----")

    def test_table_header(self):
        assert _looks_like_wiki_markup("||H1||H2||")

    def test_plain_text(self):
        assert not _looks_like_wiki_markup("Just some regular text")

    def test_plain_with_asterisk_mid_sentence(self):
        assert not _looks_like_wiki_markup("This is a * note about things")

    def test_json_not_wiki(self):
        assert not _looks_like_wiki_markup('{"key": "value"}')

    def test_multiline_with_heading(self):
        assert _looks_like_wiki_markup("Some intro\nh2. Title\nMore text")


# --- Inline markup parsing ---


class TestParseInlineMarkup:
    def test_plain_text(self):
        nodes = _parse_inline_markup("just plain text")
        assert len(nodes) == 1
        assert nodes[0] == {"type": "text", "text": "just plain text"}

    def test_bold(self):
        nodes = _parse_inline_markup("*bold*")
        assert len(nodes) == 1
        assert nodes[0]["text"] == "bold"
        assert nodes[0]["marks"] == [{"type": "strong"}]

    def test_italic(self):
        nodes = _parse_inline_markup("_italic_")
        assert nodes[0]["text"] == "italic"
        assert nodes[0]["marks"] == [{"type": "em"}]

    def test_strikethrough(self):
        nodes = _parse_inline_markup("-struck-")
        assert nodes[0]["text"] == "struck"
        assert nodes[0]["marks"] == [{"type": "strike"}]

    def test_underline(self):
        nodes = _parse_inline_markup("+under+")
        assert nodes[0]["text"] == "under"
        assert nodes[0]["marks"] == [{"type": "underline"}]

    def test_monospace(self):
        nodes = _parse_inline_markup("{{code}}")
        assert nodes[0]["text"] == "code"
        assert nodes[0]["marks"] == [{"type": "code"}]

    def test_superscript(self):
        nodes = _parse_inline_markup("^sup^")
        assert nodes[0]["text"] == "sup"
        assert nodes[0]["marks"] == [{"type": "subsup", "attrs": {"type": "sup"}}]

    def test_subscript(self):
        nodes = _parse_inline_markup("~sub~")
        assert nodes[0]["text"] == "sub"
        assert nodes[0]["marks"] == [{"type": "subsup", "attrs": {"type": "sub"}}]

    def test_link_with_text(self):
        nodes = _parse_inline_markup("[Example|https://example.com]")
        assert nodes[0]["text"] == "Example"
        assert nodes[0]["marks"] == [{"type": "link", "attrs": {"href": "https://example.com"}}]

    def test_bare_link(self):
        nodes = _parse_inline_markup("[https://example.com]")
        assert nodes[0]["text"] == "https://example.com"
        assert nodes[0]["marks"] == [{"type": "link", "attrs": {"href": "https://example.com"}}]

    def test_mention(self):
        nodes = _parse_inline_markup("[~abc123]")
        assert nodes[0] == {"type": "mention", "attrs": {"id": "abc123"}}

    def test_image_as_link(self):
        nodes = _parse_inline_markup("!screenshot.png!")
        assert nodes[0]["text"] == "screenshot.png"
        assert nodes[0]["marks"][0]["type"] == "link"

    def test_linebreak(self):
        nodes = _parse_inline_markup("before\\\\after")
        assert nodes[0] == {"type": "text", "text": "before"}
        assert nodes[1] == {"type": "hardBreak"}
        assert nodes[2] == {"type": "text", "text": "after"}

    def test_mixed_inline(self):
        nodes = _parse_inline_markup("Hello *bold* and _italic_")
        texts = [(n.get("text", ""), n.get("marks", [])) for n in nodes]
        assert texts[0] == ("Hello ", [])
        assert texts[1] == ("bold", [{"type": "strong"}])
        assert texts[2] == (" and ", [])
        assert texts[3] == ("italic", [{"type": "em"}])

    def test_empty_string(self):
        assert _parse_inline_markup("") == []

    def test_image_requires_dot(self):
        """!important! should not be treated as an image."""
        nodes = _parse_inline_markup("This is !important! news")
        assert len(nodes) == 1
        assert nodes[0]["text"] == "This is !important! news"

    def test_image_with_path(self):
        nodes = _parse_inline_markup("!images/photo.jpg!")
        assert nodes[0]["text"] == "images/photo.jpg"
        assert nodes[0]["marks"][0]["type"] == "link"

    def test_link_unsafe_scheme_javascript(self):
        """javascript: URLs should be emitted as plain text."""
        nodes = _parse_inline_markup("[click|javascript:alert(1)]")
        assert len(nodes) == 1
        assert nodes[0] == {"type": "text", "text": "[click|javascript:alert(1)]"}

    def test_link_unsafe_scheme_data(self):
        """data: URLs should be emitted as plain text."""
        nodes = _parse_inline_markup("[click|data:text/html,<script>]")
        assert len(nodes) == 1
        assert "marks" not in nodes[0]

    def test_link_safe_scheme_https(self):
        nodes = _parse_inline_markup("[Example|https://example.com]")
        assert nodes[0]["marks"] == [{"type": "link", "attrs": {"href": "https://example.com"}}]

    def test_link_safe_scheme_mailto(self):
        nodes = _parse_inline_markup("[Email|mailto:user@example.com]")
        assert nodes[0]["marks"] == [{"type": "link", "attrs": {"href": "mailto:user@example.com"}}]

    def test_link_safe_scheme_relative(self):
        nodes = _parse_inline_markup("[Page|/wiki/page]")
        assert nodes[0]["marks"] == [{"type": "link", "attrs": {"href": "/wiki/page"}}]

    def test_image_unsafe_scheme(self):
        """javascript: image URL should be emitted as plain text."""
        nodes = _parse_inline_markup("!javascript:alert.x!")
        assert len(nodes) == 1
        assert "marks" not in nodes[0]

    def test_link_unsafe_scheme_vbscript(self):
        """vbscript: URLs should be emitted as plain text."""
        nodes = _parse_inline_markup("[click|vbscript:MsgBox]")
        assert len(nodes) == 1
        assert "marks" not in nodes[0]


# --- Wiki to ADF block parsing ---


class TestWikiToAdf:
    # Headings
    def test_heading_h1(self):
        result = wiki_to_adf("h1. Title")
        block = result["content"][0]
        assert block["type"] == "heading"
        assert block["attrs"]["level"] == 1
        assert block["content"][0]["text"] == "Title"

    def test_heading_h6(self):
        result = wiki_to_adf("h6. Deep")
        assert result["content"][0]["attrs"]["level"] == 6

    def test_heading_with_inline(self):
        result = wiki_to_adf("h2. *Bold* title")
        content = result["content"][0]["content"]
        assert content[0]["marks"] == [{"type": "strong"}]
        assert content[0]["text"] == "Bold"
        assert content[1]["text"] == " title"

    # Lists
    def test_unordered_list(self):
        result = wiki_to_adf("* item1\n* item2")
        block = result["content"][0]
        assert block["type"] == "bulletList"
        assert len(block["content"]) == 2
        assert block["content"][0]["type"] == "listItem"

    def test_ordered_list(self):
        result = wiki_to_adf("# first\n# second")
        block = result["content"][0]
        assert block["type"] == "orderedList"
        assert len(block["content"]) == 2

    def test_nested_unordered_list(self):
        result = wiki_to_adf("* top\n** nested\n* bottom")
        block = result["content"][0]
        assert block["type"] == "bulletList"
        # First item should have nested list
        first_item = block["content"][0]
        assert len(first_item["content"]) == 2  # paragraph + nested list
        assert first_item["content"][1]["type"] == "bulletList"

    def test_nested_ordered_list(self):
        result = wiki_to_adf("# top\n## nested")
        block = result["content"][0]
        first_item = block["content"][0]
        assert first_item["content"][1]["type"] == "orderedList"

    # Code blocks
    def test_code_block_no_lang(self):
        result = wiki_to_adf("{code}\nprint('hi')\n{code}")
        block = result["content"][0]
        assert block["type"] == "codeBlock"
        assert block["content"][0]["text"] == "print('hi')"
        assert "attrs" not in block

    def test_code_block_with_lang(self):
        result = wiki_to_adf("{code:python}\nx = 1\n{code}")
        block = result["content"][0]
        assert block["type"] == "codeBlock"
        assert block["attrs"]["language"] == "python"
        assert block["content"][0]["text"] == "x = 1"

    def test_code_block_multiline(self):
        result = wiki_to_adf("{code}\nline1\nline2\nline3\n{code}")
        assert result["content"][0]["content"][0]["text"] == "line1\nline2\nline3"

    def test_code_block_unclosed(self):
        result = wiki_to_adf("{code}\nsome code")
        block = result["content"][0]
        assert block["type"] == "codeBlock"
        assert block["content"][0]["text"] == "some code"

    # Blockquotes
    def test_bq_shorthand(self):
        result = wiki_to_adf("bq. Some quote")
        block = result["content"][0]
        assert block["type"] == "blockquote"
        assert block["content"][0]["type"] == "paragraph"

    def test_quote_block(self):
        result = wiki_to_adf("{quote}\nquoted text\n{quote}")
        block = result["content"][0]
        assert block["type"] == "blockquote"
        assert block["content"][0]["content"][0]["text"] == "quoted text"

    def test_quote_block_unclosed(self):
        result = wiki_to_adf("{quote}\nunclosed quote")
        assert result["content"][0]["type"] == "blockquote"

    # Horizontal rule
    def test_horizontal_rule(self):
        result = wiki_to_adf("----")
        assert result["content"][0]["type"] == "rule"

    # Tables
    def test_simple_table(self):
        result = wiki_to_adf("||H1||H2||\n|c1|c2|")
        table = result["content"][0]
        assert table["type"] == "table"
        assert len(table["content"]) == 2
        # Header row
        header_row = table["content"][0]
        assert header_row["content"][0]["type"] == "tableHeader"
        # Data row
        data_row = table["content"][1]
        assert data_row["content"][0]["type"] == "tableCell"

    def test_table_header_content(self):
        result = wiki_to_adf("||Name||Age||")
        row = result["content"][0]["content"][0]
        cells = row["content"]
        assert cells[0]["content"][0]["content"][0]["text"] == "Name"
        assert cells[1]["content"][0]["content"][0]["text"] == "Age"

    def test_table_with_inline_markup(self):
        result = wiki_to_adf("||*Bold Header*||Plain||")
        cell = result["content"][0]["content"][0]["content"][0]
        para_content = cell["content"][0]["content"]
        assert para_content[0]["marks"] == [{"type": "strong"}]

    # Panel
    def test_panel(self):
        result = wiki_to_adf("{panel}\ncontent here\n{panel}")
        block = result["content"][0]
        assert block["type"] == "panel"
        assert block["attrs"]["panelType"] == "info"
        assert block["content"][0]["content"][0]["text"] == "content here"

    def test_panel_unclosed(self):
        result = wiki_to_adf("{panel}\nunclosed panel")
        assert result["content"][0]["type"] == "panel"

    # Mixed content
    def test_heading_then_paragraph(self):
        result = wiki_to_adf("h2. Title\n\nSome text")
        assert result["content"][0]["type"] == "heading"
        assert result["content"][1]["type"] == "paragraph"
        assert result["content"][1]["content"][0]["text"] == "Some text"

    def test_complex_document(self):
        doc = """h1. Project Overview

This is the *introduction*.

h2. Tasks

* Task one
* Task two
** Sub-task

h2. Code

{code:python}
def hello():
    print("world")
{code}

----

bq. Important note here"""
        result = wiki_to_adf(doc)
        types = [b["type"] for b in result["content"]]
        assert "heading" in types
        assert "paragraph" in types
        assert "bulletList" in types
        assert "codeBlock" in types
        assert "rule" in types
        assert "blockquote" in types

    def test_empty_input(self):
        result = wiki_to_adf("")
        assert result["content"] == [{"type": "paragraph", "content": []}]

    def test_version_and_type(self):
        result = wiki_to_adf("h1. Test")
        assert result["version"] == 1
        assert result["type"] == "doc"


# --- Integration: text_to_adf with wiki markup ---


class TestTextToAdfWikiIntegration:
    def test_wiki_heading_converted(self):
        result = text_to_adf("h2. Title\n\nSome paragraph")
        assert result["type"] == "doc"
        assert result["content"][0]["type"] == "heading"

    def test_wiki_list_converted(self):
        result = text_to_adf("* item 1\n* item 2")
        assert result["content"][0]["type"] == "bulletList"

    def test_plain_text_still_works(self):
        result = text_to_adf("Just plain text here")
        assert result["content"][0]["type"] == "paragraph"
        assert result["content"][0]["content"][0]["text"] == "Just plain text here"

    def test_adf_passthrough_still_works(self):
        adf = {"version": 1, "type": "doc", "content": []}
        assert text_to_adf(adf) is adf

    def test_json_adf_passthrough_still_works(self):
        adf = {"version": 1, "type": "doc", "content": []}
        assert text_to_adf(json.dumps(adf)) == adf

    def test_wiki_code_block_converted(self):
        result = text_to_adf("{code:java}\nSystem.out.println();\n{code}")
        assert result["content"][0]["type"] == "codeBlock"
        assert result["content"][0]["attrs"]["language"] == "java"

    def test_wiki_table_converted(self):
        result = text_to_adf("||Col1||Col2||\n|a|b|")
        assert result["content"][0]["type"] == "table"
