# Testing Farm Plugin Context

## What is Testing Farm?

Testing Farm is a service that provisions real hardware and VMs
for running automated tests and for interactive system access. It
supports multiple OS composes (Fedora, CentOS Stream, RHEL) across
architectures (x86_64, aarch64, s390x, ppc64le).

Tests use TMT (Test Management Tool) — a framework where test
plans and metadata live in a git repo under `.fmf/` directories.
A "compose" is an installable OS image (e.g., `Fedora-Rawhide`).

There are two main use cases:
- **Reservations**: get SSH access to a real machine for debugging,
  development, or manual testing
- **Test runs**: submit automated TMT tests against a compose

## Write Safety

All write tools default to `dry_run=true`. Always preview before
executing:

1. Call the tool with `dry_run: true` (default) to see the payload
2. Show the preview to the user
3. Only set `dry_run: false` after explicit user approval

This applies to: `testing_farm_reserve`, `testing_farm_submit_test`.

## When to Use Which Tool

**User wants a machine to SSH into** → Reservation workflow
(below). This is the most common request.

**User wants to run tests** → Test submission workflow (below).

**User asks "what machines do I have?"** →
`testing_farm_list_reservations`

**User asks "what's the IP?" or "how do I connect?"** →
`testing_farm_get_ssh` with the request ID

**User asks "what's the status?"** →
`testing_farm_get_request` — check the `state` field
(new/queued/running/complete/error). The `result` field is
`"pending"` until the request reaches a terminal state — this
is normal, not an error.

**User asks about test results** →
`testing_farm_get_results` — only useful when `state` is
`complete`. Returns per-test pass/fail from xunit. Returns a
status message for non-complete requests.
`testing_farm_get_logs` for log URLs

**User asks about available OS versions** →
`testing_farm_list_composes`

## Reservation Workflow

This is the most common workflow — getting SSH access to a system.

### Reserve a new system

1. `testing_farm_list_composes` — discover valid compose names
2. Ask the user which compose and architecture they want
3. `testing_farm_reserve` with `dry_run: true` — preview
4. Show the preview payload to the user
5. `testing_farm_reserve` with `dry_run: false` — submit
6. Wait, then `testing_farm_list_reservations` — check state
7. `testing_farm_get_ssh` — extract IP and SSH command

### Reconnect to an existing reservation

1. `testing_farm_list_reservations` — find the request ID
2. `testing_farm_get_ssh` — get the IP and SSH command

### Common composes

Use `testing_farm_list_composes` to get the full list. Common ones:
- Fedora: `Fedora-Rawhide`, `Fedora-41`, `Fedora-40`
- CentOS: `CentOS-Stream-10`, `CentOS-Stream-9`
- RHEL: `RHEL-9.3.0-Nightly`, `RHEL-10.0.0-Nightly`

### Architectures

`x86_64`, `aarch64`, `s390x`, `ppc64le`

### Duration

- Default: 60 minutes. Maximum: 720 minutes (12 hours)
- Duration is fixed at submission time — cannot be extended
- To keep the system longer, cancel and re-reserve

### Hardware specs

For specific hardware requirements:

```json
{
  "cpu": {"processors": ">= 4"},
  "memory": ">= 16 GB",
  "disk": [{"size": ">= 40 GB"}]
}
```

Most users don't need this — only use when they ask for specific
CPU/memory/disk requirements.

### SSH keys

SSH public keys from `~/.ssh/id_*.pub` are injected automatically.
The user does not need to provide keys unless they want additional
ones (e.g., a teammate's key).

## Test Submission Workflow

1. `testing_farm_submit_test` with `dry_run: true` — preview
2. Show the preview, confirm with the user
3. `testing_farm_submit_test` with `dry_run: false` — submit
4. `testing_farm_get_request` — poll for state changes (check
   `state` field: new → queued → running → complete)
5. `testing_farm_get_results` — get xunit results when complete
   (only call after state is `complete`)
6. `testing_farm_get_logs` — get log URLs for debugging failures

The `git_url` must point to a git repo containing TMT test plans
(with `.fmf/` directories). The `plan_name` parameter filters
which TMT plan to run (e.g., `/plans/smoke`).

## Monitoring and Troubleshooting

### Request states

Use `testing_farm_list_requests` with state filters:
- `new` — just submitted
- `queued` — waiting for a machine to become available
- `running` — test or reservation is active
- `complete` — finished (check result for pass/fail)
- `error` — infrastructure failure

### When SSH extraction fails

`testing_farm_get_ssh` extracts the host from results.xml
properties, guests.yaml, or console logs. If it returns an
error:
- The system may still be provisioning — wait and retry
- Check `testing_farm_get_request` to verify state is "running"
- The artifacts URL in the response can be shared with the user
  for manual inspection

### Cancelling

Use `testing_farm_cancel` to:
- Cancel a queued or running test request
- Release a reserved system before its duration expires
