# Plan: "Add project" in TheBar

Status: **planned, not started** (written 2026-10-20). Decisions at the bottom are still open.

## Context

TheBar already *shows* new projects automatically: anything started with `docker compose up` appears within a second, because the backend follows Docker's event stream. What's missing is the other direction: **creating** a project from TheBar, and editing projects that live outside the folders TheBar's container can see.

## Goal

A **+ Add** button that goes from "I want to run X" to a running card in about a minute. It writes a normal compose project to disk (so it can still be managed from the shell), validates it, starts it, and puts the card where you want it.

## User flow (one dialog, 4 steps)

1. **Source.** Pick one:
   - **Paste** a `docker-compose.yml`. This covers most cases, since nearly every project's README has one.
   - **Convert a `docker run …` command** into compose (common flags: `-p -v -e --name --restart --network`).
   - **Template** from a small built-in list (linuxserver apps, Uptime Kuma, Vaultwarden…), based on `knownApps` in `backend/known.go`.
   - **Adopt an existing folder** that has a compose file but isn't running yet.
2. **Details**
   - **Name:** becomes the project name and folder, `/opt/<name>` by default.
   - **Ports:** each published port is checked against what's in use (same data as the Ports dialog, `UsedPorts()` in `backend/extras.go`). Conflicts are shown in red, with a "use next free port" button.
   - **Volumes:** relative paths like `./config` are shown as their real target folder, and the folders are created.
   - **linuxserver images:** `PUID`/`PGID` default to 1000 (the host user).
   - **.env editor**, with a "generate secret" button for database passwords.
3. **Card.** Nickname, group (existing, new, or none) and position, set **before** starting, so the card appears in the right place with the right name.
4. **Review & start**
   - Shows the final files, validated with `docker compose config`.
   - **Create & start** writes the files, then runs `pull` and `up -d` as a job. The output streams in the job console.
   - When the web port responds, the card highlights and offers **Open**.

## Backend work

- **Project roots.** Folders where TheBar may create projects: default `/opt`, configured with `THEBAR_PROJECT_ROOTS`. The name is validated (no `..`, folder must not exist) and nothing is written outside a root.
- **Ownership.** New folders and files get uid/gid 1000 (configurable, e.g. `THEBAR_PUID`/`THEBAR_PGID`), so they can be edited from the shell without sudo.
- **Endpoints:**
  - `POST /api/projects/validate`: lint the compose file, list its ports, volumes and images, report conflicts. Reuse the temp-file validation from `Compose.Save` in `backend/compose.go`.
  - `POST /api/projects`: write the files and start them as a job (`Jobs.Start` in `backend/jobs.go`).
  - `GET /api/templates`.
- **Pre-placing the card.** Write the layout entry for `project/service` before the containers exist. Cards are already keyed that way (see `rawCards` in `frontend/src/lib/layout.ts`), so this needs no special handling.
- **Rollback.** If `up` fails, offer to delete the half-created folder, or keep it to fix in the Config tab.

## Projects outside the mounted folders

TheBar can only edit files it can see, and a running container can't gain new mounts. So:

- When a project's compose file isn't visible inside the container, the card's Config tab says so and shows the exact volume line to add to TheBar's `docker-compose.yml`, instead of showing a read error.
- Projects created through TheBar always land inside a mounted root, so this only affects older projects.

## Phases

1. **Foundation:** project roots, validate endpoint, create + start job, "not mounted" hint in the Config tab. *Small.*
2. **Wizard:** paste source, port and volume checks, .env editor, card placement, review step. *Medium. This is the core of the feature.*
3. **Conveniences:** `docker run` converter, templates, secret generator, adopting existing folders. *Small to medium each, all optional.*
4. **Lifecycle (later):**
   - **Remove project:** `down`, keeping volumes by default and archiving the folder instead of deleting it.
   - **Create the Nginx Proxy Manager proxy host automatically.** This needs NPM API credentials; today TheBar only reads NPM's generated config files.

## Risks

- **Pasted compose files run whatever images they name.** The review step lists every image and every host path being mounted; host paths outside the project folder get a warning.
- **Compose `profiles`, `extends` and `include`** validate fine but make the review harder to read. Show the resolved `docker compose config` output next to the file.

## Open decisions

1. Should new projects go in `/opt/<name>`, owned by the host user (1000:1000)?
2. Paste-only first (phases 1–2), or templates and the `docker run` converter right away?
3. Should **Remove project** be part of this, or wait?
