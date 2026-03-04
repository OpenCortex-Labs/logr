# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < 0.1.0 | :x:                |

We support only the latest released version with security patches. We strongly recommend always using the latest version.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

If you discover a security vulnerability in `logr`, please report it responsibly using one of the following methods:

### GitHub Private Vulnerability Reporting (Preferred)

Use GitHub's built-in private vulnerability reporting:

1. Go to the [Security tab](https://github.com/Mihir99-mk/logr/security) of this repository.
2. Click **"Report a vulnerability"**.
3. Fill in the details of the vulnerability.

This creates a private advisory that is only visible to the maintainers. We will acknowledge your report within **48 hours** and provide an estimated timeline for a fix.

### What to Include

A good vulnerability report includes:

- A description of the vulnerability and its potential impact.
- Steps to reproduce the issue (proof-of-concept if possible).
- The version of `logr` you tested against.
- Your OS and architecture.
- Any suggested mitigations or fixes.

## Disclosure Policy

- We will acknowledge your report within **48 hours**.
- We will keep you informed of progress toward a fix.
- We will credit you in the release notes unless you prefer to remain anonymous.
- We ask that you give us a reasonable amount of time (typically **90 days**) to address the issue before any public disclosure.

## Scope

The following are in scope:

- The `logr` CLI binary and its dependencies.
- The embeddable `logr/logr` logger package.
- The `logr/run` watch-mode integration package.

The following are **out of scope**:

- Vulnerabilities in third-party dependencies (please report these upstream).
- Issues that require physical access to the machine running `logr`.
- Social engineering attacks.
