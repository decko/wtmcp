# wtmcp

MCP server with a language-agnostic plugin system. Plugins are simple
executables (Python, bash, or any language) that communicate with the
core over JSON-lines on stdin/stdout. The core handles auth, HTTP
proxying, caching, and output encoding so plugins stay minimal.

## Architecture

```
┌─────────────────────────────────────────────────┐
│  wtmcp (Go)                                     │
│                                                 │
│  MCP Server ─── Plugin Manager ─── HTTP Proxy   │
│  (mcp-go)       Discovery         Auth inject   │
│                 Lifecycle          TLS verify   │
│                 Dispatch           Rate limit   │
│                                                 │
│              Cache Store ─── Auth Providers     │
│              (memory/fs)     Bearer, Basic,     │
│                              Kerberos, OAuth2   │
└────────┬──────────────────────────┬─────────────┘
         │ stdio (MCP/JSON-RPC)     │ stdin/stdout (JSON-lines)
    ┌────┴────┐               ┌─────┴──────────┐
    │ AI      │               │ Plugins        │
    │ Client  │               │ Zero deps      │
    └─────────┘               │ No HTTP libs   │
                              │ No auth code   │
                              └────────────────┘
```

## Features

- **Plugin protocol**: JSON-lines over stdin/stdout, any language
- **Auth**: Bearer, Basic, Kerberos/SPNEGO, OAuth2 with token refresh,
  auto-detection from available credentials
- **HTTP proxy**: Auth injection, domain validation, TLS enforcement,
  binary response encoding, multipart upload support
- **Cache**: In-memory store with namespace isolation and TTL
- **Output**: TOON encoding for ~40% token savings (optional)
- **Plugin setup**: Manifest-declared wizard metadata for CLI tooling
- **Progressive discovery**: Tools default to deferred; only primary
  tools are loaded into model context. Deferred tools are
  discoverable via `tool_search` and called directly through MCP
- **Encrypted credentials**: Ansible Vault encrypted env.d files,
  auto-detected and decrypted transparently at startup

## Building and Running

```bash
make build

# Run with a workdir (default: ~/.config/wtmcp)
./wtmcp --workdir ~/.config/wtmcp
```

The workdir layout:

```
~/.config/wtmcp/
  config.yaml           Core config (optional)
  .env                  Environment variables
  env.d/*.env           Additional env files
  plugins/
    jira/
      plugin.yaml       Plugin manifest
      handler.py        Plugin executable
```

## Writing Plugins

A plugin is a directory with a manifest (`plugin.yaml`) and a handler
executable. The core discovers plugins, starts handlers as child
processes, and routes tool calls over stdin/stdout using JSON-lines.

See [docs/plugin-guide.md](docs/plugin-guide.md) for the full guide
with examples in multiple languages.

### Minimal Example (bash)

A oneshot plugin that runs the handler once per tool call:

**plugin.yaml:**
```yaml
name: hello
version: "1.0.0"
description: "A greeting plugin"
execution: oneshot
handler: ./handler.sh
tools:
  - name: hello_world
    description: "Says hello to someone"
    params:
      name:
        type: string
        default: "World"
        description: "Who to greet"
enabled: true
```

**handler.sh:**
```bash
#!/bin/bash
read -r INPUT
ID=$(echo "$INPUT" | jq -r '.id')
NAME=$(echo "$INPUT" | jq -r '.params.name // "World"')

echo "{}" | jq -c --arg id "$ID" --arg name "$NAME" \
  '{id: $id, type: "tool_result", result: {message: ("Hello, " + $name + "!")}}'
```

### API Plugin Example (Python)

A persistent plugin that calls an API through the core's HTTP proxy.
The handler stays running and processes multiple tool calls. Auth
headers are injected automatically — the plugin never sees tokens.

**plugin.yaml:**
```yaml
name: myapi
version: "1.0.0"
description: "Example API plugin"
execution: persistent
handler: ./handler.py

services:
  auth:
    type: bearer
    token: "${MY_API_TOKEN}"
  http:
    base_url: "${MY_API_URL}"

tools:
  - name: myapi_get_status
    description: "Get API status"
    params: {}
  - name: myapi_search
    description: "Search the API"
    params:
      query:
        type: string
        required: true
enabled: true
```

**handler.py:**
```python
#!/usr/bin/env python3
import json, sys

def _send(msg):
    print(json.dumps(msg, separators=(",", ":")), flush=True)

def _recv():
    line = sys.stdin.readline()
    if not line:
        sys.exit(0)
    return json.loads(line.strip())

def http(method, path, query=None):
    msg = {"id": "1", "type": "http_request", "method": method, "path": path}
    if query:
        msg["query"] = query
    _send(msg)
    resp = _recv()
    return resp.get("status", 0), resp.get("body", {})

def get_status(_params):
    status, body = http("GET", "/status")
    return body

def search(params):
    status, body = http("GET", "/search", query={"q": params["query"]})
    return body

TOOLS = {"myapi_get_status": get_status, "myapi_search": search}

while True:
    msg = _recv()
    if msg.get("type") == "init":
        _send({"id": msg["id"], "type": "init_ok"})
    elif msg.get("type") == "shutdown":
        _send({"id": msg["id"], "type": "shutdown_ok"})
        break
    elif msg.get("type") == "tool_call":
        fn = TOOLS.get(msg.get("tool"))
        if fn:
            result = fn(msg.get("params", {}))
            _send({"id": msg["id"], "type": "tool_result", "result": result})
        else:
            _send({"id": msg["id"], "type": "tool_result",
                   "error": {"code": "unknown_tool", "message": msg.get("tool")}})
```

### Key Concepts

- **Oneshot** plugins are spawned per tool call. Simplest to write.
- **Persistent** plugins start once and handle many calls via a main loop.
- **HTTP proxy**: plugins send `http_request` messages, the core makes
  the call with auth and returns `http_response`. No HTTP library needed.
- **Cache**: plugins send `cache_get`/`cache_set` messages. The core
  manages storage and TTL.
- **Auth variants**: a single plugin can support multiple auth methods
  (e.g., Cloud Basic + Server Bearer + Kerberos) with auto-detection.

## Plugin Management

Plugins can be reloaded at runtime without restarting the server.

**From an AI assistant:**
```
plugin_reload(name="jira")
plugin_list()
```

**From a terminal** (control directory):
```bash
touch ~/.config/wtmcp/control/commands/reload-jira
touch ~/.config/wtmcp/control/commands/reload-all
```

Results appear in `~/.config/wtmcp/control/results/`. The server writes
its PID to `~/.config/wtmcp/control/mcp.pid` for process tracking.

MCP clients are automatically notified when tools or resources change.

### OAuth Plugin Management

Plugin authentication (particularly for OAuth-enabled plugins) is managed through the `wtmcpctl` command-line utility. See [README-wtmcpctl.md](README-wtmcpctl.md) for usage instructions and setup.

## Encrypted Credentials

env.d files can be encrypted with [Ansible Vault](https://docs.ansible.com/ansible/latest/vault_guide/)
for at-rest protection. The server auto-detects encrypted files by
magic header and decrypts them transparently at startup. Plugins
receive plaintext credentials as usual — no plugin changes needed.

### Quick Start

```bash
# Create a vault password file (umask prevents brief permission race)
(umask 077 && openssl rand -base64 32 > ~/.vault-pass)

# Tell wtmcp where the password file is
# (add to ~/.config/wtmcp/config.yaml)
#   secrets:
#     vault_password_file: ~/.vault-pass

# Encrypt an env.d file
ansible-vault encrypt --vault-password-file ~/.vault-pass \
    ~/.config/wtmcp/env.d/jira.env

# Start the server — decrypts automatically
wtmcp
```

Encrypted files can be safely committed to git, shared, or backed
up. Anyone who obtains them still needs the vault password to
decrypt.

### Password Sources

The vault password is resolved in priority order:

1. `WTMCP_VAULT_PASSWORD` environment variable (CI/CD convenience)
2. `WTMCP_VAULT_PASSWORD_FILE` environment variable (path to file)
3. `secrets.vault_password_file` in config.yaml (recommended)

For production and workstations, prefer file-based passwords. Env
vars are intended for CI/CD pipelines where mounting a file is
inconvenient.

### Multi-Password Support (Vault IDs)

Ansible Vault 1.2 supports labeled passwords (vault IDs). Different
env.d files can use different passwords:

```bash
# Encrypt with a vault ID label
ansible-vault encrypt --vault-id prod@~/.vault-pass-prod \
    ~/.config/wtmcp/env.d/jira.env
```

Configure per-ID password files in config.yaml:

```yaml
secrets:
  vault_password_file: ~/.vault-pass          # default
  vault_ids:
    prod: ~/.vault-pass-prod
    dev: ~/.vault-pass-dev
```

Per-ID env vars are also supported: `WTMCP_VAULT_PASSWORD_PROD`,
`WTMCP_VAULT_PASSWORD_DEV`.

If no per-ID password is found, the server falls back to the
default password chain automatically.

### Diagnostics

```bash
wtmcp check
```

Reports vault password status and per-group encryption details
(only encrypted groups are shown):

```
vault password: file (~/.vault-pass)
  - jira (encrypted, vault 1.1, decryption ok)
  - snyk (encrypted, vault 1.2 id=prod, decryption failed)
```

### Migrating Existing Files

1. Create a vault password file (see Quick Start)
2. Configure `secrets.vault_password_file` in config.yaml
3. Encrypt one env.d file:
   `ansible-vault encrypt --vault-password-file ~/.vault-pass env.d/jira.env`
4. Verify: `wtmcp check` should show "decryption ok"
5. Repeat for remaining files
6. Optionally commit encrypted files to git

A single env.d directory can mix plaintext and encrypted files.
Migrate incrementally — one file at a time.

If env.d files were previously committed in plaintext, encrypting
them does not remove the plaintext from git history. Rotate the
affected credentials after migrating and consider using
`git filter-repo` to remove the old plaintext from history.

### Reloading Encrypted Credentials

Credential changes take effect on `plugin_reload` without a server
restart. The vault password is re-read from its source on each
reload, so password rotations are picked up automatically.

### Security Notes

- Ansible Vault uses AES-256-CTR with PBKDF2-SHA256 (10,000
  iterations). Use strong passwords (20+ characters or
  `openssl rand -base64 32`) to compensate for the low iteration
  count.
- **Back up your vault password file.** Losing it means permanent
  loss of access to encrypted credentials. Store a copy in a
  separate secure location.
- Ansible Vault is a practical improvement for development and
  CI/CD. Regulated environments requiring key rotation, audit
  logging, or FIPS-validated crypto should use HashiCorp Vault or
  cloud KMS.

### Credential File Encryption

In addition to env.d files, credential files in
`credentials/<group>/` can also be vault-encrypted. Supported
files:

- `client-credentials.json` (OAuth2 client credentials)
- TLS `client_cert` and `client_key` PEM files

Token files (`token-*.json`) are **not** encrypted — they are
auto-rotated, short-lived, and derived from the client credentials.

Encrypted credential files are decrypted to memory-backed file
descriptors (memfd on Linux, unlinked tmpfile on macOS) so
decrypted content never touches persistent storage. Plugins
receive the same file paths as usual — no plugin changes needed.

### wtmcpctl vault Commands

Encrypt and decrypt files without requiring `ansible-vault`:

```bash
# Encrypt a file
wtmcpctl vault encrypt env.d/jira.env

# Encrypt with vault ID
wtmcpctl vault encrypt --vault-id prod env.d/jira.env

# Decrypt a file
wtmcpctl vault decrypt env.d/jira.env

# Verify decryption without writing
wtmcpctl vault decrypt --check env.d/jira.env

# View decrypted content without modifying the file
wtmcpctl vault view env.d/jira.env
```

Password is sourced from `--vault-password-file`, `WTMCP_VAULT_PASSWORD`
env var, config.yaml, or interactive prompt (with echo suppression).

## Included Plugins

### Google Plugins

Google plugins provide access to Google Workspace services using OAuth2
authentication:

| Plugin | Description |
|--------|-------------|
| google-drive | File metadata, search, and export |
| google-calendar | Calendar events and management |
| google-gmail | Email reading and sending |

All Google plugins require OAuth2 authentication. See [README-wtmcpctl.md](README-wtmcpctl.md)
for setup instructions.

### Jira Plugin

The included Jira plugin covers read, write, sprint, and export
operations:

| Category | Examples |
|----------|---------|
| Read | `jira_search`, `jira_get_myself`, `jira_get_transitions` |
| Write | `jira_create_issue`, `jira_add_comment`, `jira_assign_issue` |
| Sprint | `jira_list_available_sprints`, `jira_get_sprint_issues` |
| Export | `jira_export_sprint_data`, `jira_download_attachment` |

All write tools default to `dry_run=true`. Cloud-aware (ADF format,
accountId assignments). Auth variants: Cloud Basic, Server Bearer,
Server Kerberos.

## Progressive Tool Discovery

By default (`tools.discovery: full`), all tools are loaded into the
model's context. With progressive discovery, only primary tools are
loaded; deferred tools are discoverable via `tool_search`.

Enable in `config.yaml`:

```yaml
tools:
  discovery: progressive
```

Plugin authors mark key tools with `visibility: primary` in
`plugin.yaml`. All other tools default to deferred. See
[docs/plugin-guide.md](docs/plugin-guide.md) for details.

## Testing

```bash
# Go core tests
go test ./...

# Python plugin tests
.venv/bin/pytest tests/ -v

# All pre-commit checks
pre-commit run --all-files
```

## Project Layout

```
cmd/
  wtmcp/                MCP server entry point
  wtmcpctl/             Plugin management CLI tool
internal/
  auth/                 Auth providers (bearer, basic, kerberos, oauth2)
  cache/                Key-value cache with TTL
  config/               Env var resolution, YAML config
  encoding/             TOON output encoding
  google/               Google OAuth helper (shared by Google plugins)
  plugin/               Manager, manifest, transport, dispatch
  protocol/             Wire protocol message types
  proxy/                HTTP proxy with auth injection
  server/               MCP server integration
plugins/
  google-drive/         Google Drive plugin (Go)
  google-calendar/      Google Calendar plugin (Go)
  google-gmail/         Gmail plugin (Go)
  jira/                 Jira plugin (Python, zero external deps)
  confluence/           Confluence plugin (Python)
  gitlab/               GitLab plugin (Python)
tests/
  plugins/              Plugin unit tests
docs/
  plugin-guide.md       Plugin development guide
  wtmcpctl.md           OAuth management tool guide
```

## License

This project is licensed under the GNU General Public License v3.0.
See [LICENSE](LICENSE) for the full text.
