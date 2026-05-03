# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in wtmcp, please report it
responsibly. **Do not open a public GitHub issue.**

**Email:** [scorreia@redhat.com](mailto:scorreia@redhat.com)

Include:

- Description of the vulnerability
- Steps to reproduce
- Affected version(s)
- Impact assessment (if known)

## Response Timeline

- **Acknowledgement:** within 3 business days
- **Initial assessment:** within 7 business days
- **Fix or mitigation:** timeline communicated after assessment

## Scope

The following are in scope for security reports:

- Authentication and authorization bypass
- Credential leakage or exposure
- Sandbox escape or privilege escalation
- Prompt injection leading to unauthorized actions
- Command injection or SSRF
- Plugin isolation failures
- Denial of service via resource exhaustion

The following are **out of scope**:

- Vulnerabilities in upstream dependencies (report to the
  upstream project directly)
- Issues requiring physical access to the host
- Social engineering attacks

## Supported Versions

Security fixes are applied to the latest release on the `main`
branch. Older versions are not actively maintained.

## Disclosure Policy

We follow coordinated disclosure. After a fix is available, we
will credit the reporter (unless they prefer anonymity) and
publish a description of the vulnerability.
