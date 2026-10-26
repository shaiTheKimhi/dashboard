# File Management Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement task-by-task.

**Goal:** Create and delete files and directories from the current dashboard.
**Architecture:** Reuse rooted filesystem operations, write guards, file mutation lock and native dialogs.
**Tech Stack:** Go standard library, HTML/CSS/JavaScript and existing Chromium DevTools checks.
**Spec:** docs/superpowers/specs/2026-10-24-file-management-design.md

## Global Constraints

- Linux ARM64, Go 1.25+, no new dependencies; retain the existing non-Git project.
- Preserve /home/shai root, credentials, TLS and installed user service.
- Never replace entries during create; never delete root; deletion of links never follows targets.

## Review Focus

- Traversal, root aliases and symlink replacements must not escape the selected tree.
- Duplicate names and name validation must not damage existing files.
- Recursive deletion must include hidden entries while leaving symlink targets untouched.
- Confirmation must name the actual target; stale revisions and wrong confirmation names must fail.
- Failed requests, hidden creations, unsaved editor changes and mobile layouts must remain usable.

## Task 1: Backend

Files: new manage.go/manage_test.go/manage_http_test.go, shared mutation_http.go; edit_http.go, edit.go, server.go.
Interfaces: CreateEntry(root,path,kind), InspectEntry(root,path), DeleteEntry(root,path,revision,name); POST/GET/DELETE /api/entry.
- [x] Write failing tests for create/delete round trips, collisions, invalid names, root/boundaries, recursive content, symlinks, stale confirmations and permissions.
- [x] Implement filesystem operations using pinned parents and the existing mutation lock; pass targeted tests.
- [x] Write failing HTTP tests; share JSON/origin/header guards with editing and add routes.
- [x] Run all Go tests and vet.

## Task 2: UI and deployment

Files: static/index.html, static/app.js, static/style.css, scripts/manage_check.py, README.md, docs/acceptance.md.
- [x] Add browser checks and observe failure before implementing controls/dialogs.
- [x] Add create and delete dialogs, refresh listings and open newly created files in editor.
- [x] Build, atomically deploy with backup, restart without changing configuration.
- [x] Run backend checks, installer regression and all browser suites; obtain focused independent review.
- [x] Document permanent recursive deletion and current behavior; verify service and remove disposable fixtures/browser.
