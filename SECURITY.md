# Security policy

TheBar controls Docker on the host it runs on, so security reports are welcome and taken seriously.

## Reporting a vulnerability

Please **don't open a public issue** for security problems. Use GitHub's private reporting instead:
**Security → Report a vulnerability** on this repository.

Include what you found, how to reproduce it, and what version or commit you tested. You'll get an answer as soon as I can manage; this is a small hobby project, so please be patient.

## What counts

- Getting access without the password, or reaching someone else's session
- Reading or writing files outside TheBar's data folder and the compose files it manages
- Running commands or reaching the Docker API without being logged in
- Cross-site attacks (CSRF, XSS, DNS rebinding) against a logged-in or a not-yet-set-up TheBar

## What doesn't

- Everything a logged-in user can do. That is the product: start, stop and edit containers.
- Exposing TheBar to the internet. It's built for a trusted local network; see the Security section of the README.
- Missing features such as multiple users or two-factor login.

## Supported versions

Only the latest commit on `main`.
