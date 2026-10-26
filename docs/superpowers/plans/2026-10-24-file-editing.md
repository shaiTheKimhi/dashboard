# File Editing Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Edit existing text files in the browser and expose external SFTP editing details.
**Architecture:** Rooted Go filesystem operations and authenticated JSON endpoints, consumed by native browser dialogs.
**Tech Stack:** Go standard library, HTML/CSS/JavaScript, existing Chromium checks.
**Spec:** docs/superpowers/specs/2026-10-24-file-editing-design.md

## Global Constraints

- No new dependencies; Linux ARM64 Nano compatibility.
- Existing UTF-8 files only, maximum 1 MiB; preserve the configured filesystem boundary.
- No Git initialization; work in the existing project as established in its original approved plan.
- Preserve installed /home/shai root, TLS and credentials when replacing the binary.

## Review Focus

- Stale revisions and concurrent saves must not silently overwrite each other.
- Path traversal, symlink swapping, special files and hard links must not write outside the intended file.
- Bad JSON, excessive bodies and cross-origin writes must be rejected.
- Unsaved text must survive save errors and cancellation prompts.
- CRLF, Unicode paths, empty files, read-only files and narrow screens must work.

## Task 1: Rooted text editing and HTTP endpoints

Files: new edit.go/edit_test.go; modify server.go/server_test.go.
Interfaces: ReadText(root, path), SaveText(root, path, revision, content); GET/PUT /api/edit and GET /api/external.
- [x] Write failing tests for text round trips, revisions, line endings, limits, links, special files, permissions and simultaneous saves.
- [x] Implement minimal bounded reads and atomic saves; verify targeted tests.
- [x] Write failing HTTP tests for auth, CSRF/JSON/body validation, conflict status and external connection metadata.
- [x] Add endpoints and run full Go tests and vet.

## Task 2: Browser dialogs and deployment

Files: static/index.html, static/app.js, static/style.css, scripts/browser_check.py, README.md, docs/acceptance.md.
- [x] Extend browser checks for edit/save/cancel/unsaved/conflict/unsupported files/external path and mobile layout; observe failure before UI implementation.
- [x] Add native dialogs and file actions, preserving the existing hidden-file filter and download behavior.
- [x] Build and atomically install, retaining the previous binary and current configuration; restart the user service.
- [x] Run Go tests/vet, installer regression and full browser checks; review code and fix material findings.
- [x] Document editing, SSH credentials, file limits and fresh acceptance results; close disposable browser.
