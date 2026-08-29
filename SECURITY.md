# Security Policy

## Supported Versions

Security fixes are provided for the latest stable release line. Because Pulse
Agent is currently pre-1.0, compatibility and support guarantees may change
between minor releases.

| Version | Supported |
| --- | --- |
| `0.11.x` | Yes |
| Older versions | No |

Users should upgrade to the latest release before investigating or reporting a
potential issue. The updater verifies release checksums and the version of a
downloaded binary before installing it.

## Reporting a Vulnerability

Please do not report security vulnerabilities through public GitHub issues,
pull requests, or discussions. Public disclosure can put users at risk before
a fix is available.

Use GitHub Security Advisories to submit a private report:

<https://github.com/Nimweo/pulse-agent/security/advisories/new>

If GitHub Security Advisories are unavailable, contact the maintainers through
the private contact mechanism listed in the repository owner profile and do not
include sensitive credentials in the initial message.

Please include as much of the following information as possible:

- affected Pulse Agent version and operating system,
- relevant configuration, with API keys and other secrets removed,
- clear reproduction steps or a minimal proof of concept,
- expected and observed behavior,
- security impact and realistic attack prerequisites,
- logs, stack traces, or network traces that help reproduce the issue,
- a suggested mitigation or patch, if available.

## What to Report

Examples of issues that should be reported privately include:

- API key disclosure or incorrect authorization behavior,
- unintended execution of downloaded update content,
- checksum or release verification bypasses,
- path traversal, arbitrary file writes, or unsafe configuration handling,
- privilege escalation in the Linux installer or systemd updater,
- vulnerabilities that expose collected host or process information.

Please report ordinary bugs, feature requests, and configuration questions as
public GitHub issues when they do not create a security risk.

## Response and Disclosure

Reports are reviewed on a best-effort basis. We will acknowledge a report when
possible, investigate its impact, and coordinate a fix and disclosure timeline
with the reporter. Please allow reasonable time for affected users to update
before sharing technical details publicly.

Do not perform testing that could damage systems, access data belonging to
other users, degrade availability, or exfiltrate real credentials. Use an
isolated test environment and test accounts whenever possible.

## Security Updates

Security fixes are announced in the GitHub release notes. Update instructions
are documented in [README.md](README.md). Users should rotate any credential
that may have been exposed and upgrade promptly after a relevant advisory.
