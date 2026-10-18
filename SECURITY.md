# Security policy

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through [GitHub's vulnerability reporting](https://github.com/feruzabad/debroxy/security/advisories/new) instead, with the affected version, the impact and the steps to reproduce.

You get an answer within a week. Once a fix is released, the advisory is published, with credit to you unless you prefer otherwise.

## Supported versions

Only the latest release receives security fixes.

## Scope

In scope are debroxy itself and its container image. Particularly sensitive are anything that exposes a provider API key or CDN URL to users, lets a user see or affect another user's jobs, or bypasses authentication, quotas or rate limits. Problems in TorBox or Real-Debrid themselves belong to those providers.
