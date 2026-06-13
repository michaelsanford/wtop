# Security Policy

## Supported Versions

Only the **latest release** of `wtop` receives security fixes. Older versions are not patched.

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |
| < Latest | No       |

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Instead, use [GitHub's private vulnerability reporting](https://github.com/michaelsanford/wtop/security/advisories/new) to submit a report confidentially.

Include as much detail as you can:

- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept
- The `wtop` version affected
- Your suggested severity (if you have one)

## Response SLAs

| Milestone | Target |
|-----------|--------|
| Acknowledgement | Within **21 days** of a valid report |
| Fix | **Best effort** — complexity, severity, and maintainer availability determine timeline |

There are no guaranteed fix timelines. Severe, easily exploitable vulnerabilities will be prioritised.

## Scope

This project is a local Windows TUI application with no network-facing components or privileged system access beyond what the host OS exposes to standard user processes. The attack surface is limited, but reports involving:

- Unsafe handling of process/system data from the Windows API
- Supply-chain concerns (dependency confusion, compromised build artifacts)
- Malicious input via environment or configuration

...are in scope and welcome.

## Bug Bounty

There is **no bug bounty program**. This is a personal open-source project maintained without commercial backing.

## Credit

Reporters of validated vulnerabilities will be **credited by name (or handle) in the release notes** and security advisory, unless anonymity is requested.
