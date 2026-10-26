# Device acceptance record — 2026-10-23

## Environment and installation

NVIDIA Jetson Nano Developer Kit, ARM64, Ubuntu 18.04.6, L4T R32.7.1, kernel 4.9.253-tegra. The checksum-verified official Go 1.27.3 toolchain runs natively. Production binary uses `CGO_ENABLED=0`, `GOARCH=arm64`, `GOARM64=v8.0`.

Installed address: `https://192.168.68.107:8443`. The systemd user service is enabled and running, with linger enabled for startup before login. Password, digest and TLS key live outside the shared directory in protected configuration files. Certificate validity: 2026-10-23 through 2027-10-23.

## Checks performed

| Check | Evidence/result |
| --- | --- |
| Go tests | 13 top-level tests with subtests; `go test ./...` passes |
| Go static checks | `go vet ./...` passes |
| Installer regression | Python standard-library unittest passes; system binary directory remains searchable with private umask |
| ARM64 compatibility | Built and executed natively; unauthenticated health 401 and authenticated health 200 |
| HTTPS and authentication | Certificate-verified local LAN-address requests succeed; page, health, assets, metrics and files all require credentials |
| Filesystem boundary | Traversal, malformed/duplicate encoding, escaping/absolute symlinks and symlink replacement tests pass |
| Special files/permissions | FIFO is rejected without blocking; directories cannot download; unreadable files fail as the unprivileged user |
| Downloads | 128 MiB source/download SHA-256 match; interruption leaves telemetry responsive; HEAD, byte ranges and two-download admission tested |
| GPU scale | Short CUDA validation: sysfs load 997 → 99.7%; contemporaneous `tegrastats` reports 99%. No persistent GPU process added |
| Browser | Chromium 97.0.4692.71, seven automated checks pass using a disposable headless profile |
| Desktop/mobile | Screenshots inspected at 1440×1200 and 390×844; no page overflow at mobile width |
| Unsafe/special filenames | HTML-like text renders literally; Unicode/space names download with correct content |
| Browser lifecycle | Empty/parent navigation, obsolete-response handling, visibility pause/resume, offline stale state and reconnect all pass |
| systemd | Final binary reinstall/restart succeeds; unit active/running and enabled, exit status0; linger=yes |

The Go suite also checks absent/expired/future/mismatched TLS material, non-finite telemetry, JSON failure responses, first-sample/reset counters, immutable snapshot copies and truncated listings. The final installed binary's SHA-256 matches the final build, protected config directory mode is 0700, credentials/environment/key are 0600, and all verification fixtures have been removed from the export. All 13 Go tests, `go vet`, the installer regression and all seven browser checks passed again after the fixes and final reinstall.

Resource measurement: 301.25 seconds, 149 authenticated requests at a 2-second cadence, maximum RSS **14.34 MiB**, average CPU **0.767% of one core**. The 128 MiB download peaked at **14.23 MiB RSS**, with **0.00 MiB** measured growth above its baseline. These meet the planned 100 MiB/5% targets. The final change only rejects invalid optional readings and adds JSON-error handling; normal telemetry behavior is unchanged from the measured binary. The final binary is separately rebuilt, installed, restarted and checked again.

Machine-readable results and screenshots are in ignored `build/`: `browser-check.json`, `device-check.json`, `gpu-check.json`, `dashboard-desktop.png` and `dashboard-mobile.png`.

## Review and decisions

A fresh reviewer found no critical application defects. Its system-installer directory-permission finding was reproduced with a private-umask regression test and fixed. Optional non-finite telemetry was also treated as requiring a fix because it could invalidate the entire metrics response: a failing sensor test and failing JSON-response test now pass with the full suite. No review findings are deferred.

- Work remains in the original project without initializing Git, as specified by the plan. There is no branch/commit history; tests and this record provide the verification trail.
- Installation uses the existing user's persistent service because sudo requires an unavailable password. The dedicated-account system installer is supplied and its filesystem permission logic tested, but actual system installation requires an administrator. User mode has the application's file boundary but lacks dedicated-account OS isolation.
- The race detector was attempted and cannot run on this Nano's 39-bit VMA (`Found 39 - Supported 48`). Run it on a compatible Linux host; no race-clean claim is made.
- `systemd-analyze verify` cannot initialize its temporary manager cgroup in this unprivileged systemd 237 session. Actual unit loading, startup and restart are the available validation.

## Operator checks remaining

Open the address from a second physical LAN device, install/trust the certificate on that client, and sign in using the private credentials file. Schedule an actual reboot to verify startup in your network environment. A full external-internet disconnection test was not performed; all application assets are embedded and the runtime has no external service calls. No firewall rules or router forwarding were changed.

The running service can be used now. These checks concern the client's trust/network environment and the next reboot rather than unimplemented application features.

## Configuration update — 2026-10-24

At the owner's request, changed the installed user service's `DASHBOARD_FILE_ROOT` from the empty `/home/shai/dashboard-share` to `/home/shai` and restarted it. The configuration remains mode 0600. Authenticated, certificate-verified requests list all 35 home entries, including hidden entries; nested project-folder navigation and a file download matching its source bytes pass. Unauthenticated listing still returns 401, and parent traversal returns 400. The service is active after restart. The expanded root deliberately includes readable private home configuration and credentials; this supersedes the original dedicated-export deployment choice. Application code and binary are unchanged.

## Hidden-file display toggle — 2026-10-24

Added a native “Show hidden files” checkbox beside Refresh, unchecked on page load. It filters dot-prefixed files and directories together, preserves parent navigation, counts visible entries, and explains folders containing only hidden items. The setting applies during folder navigation and resets on reload. This is a display filter; hidden files remain accessible to authenticated requests.

The browser regression first failed because the checkbox was missing, then passed on the rebuilt, deployed binary. All eight Chromium browser checks pass, covering default hiding, toggling both entry types, counts, nested hidden directories, hidden-only status, parent navigation, existing filename/download checks, obsolete requests, mobile width, polling visibility, and offline recovery. Fresh Go tests, `go vet`, the installer unittest, and the ARM64 build also pass. A read-only reviewer found no important correctness issues. The service was restarted successfully and retains `/home/shai` as its file root.

## File editing — 2026-10-24

At the owner's request, added Edit and External edit actions beside file downloads. The plain browser editor supports existing UTF-8 text up to 1 MiB, Save/Cancel, unsaved-change prompts and stale-revision errors that preserve typed text. Backend operations reject linked/special files, use a pinned parent with strict no-follow leaf opens, serialize dashboard saves, and replace via a synced temporary file. Ordinary permission bits, owner/group and consistent CRLF line endings are retained. Advanced filesystem metadata is not preserved; README documents using an external editor when that matters. The new atomic rooted Rename API raises the Go minimum to 1.25; the installed 1.27.3 compiler satisfies this.

New Go tests first failed for missing editor APIs/routes, then passed for text/empty/Unicode/CRLF round trips, atomic replacement, original permission/ownership retention, invalid/oversized text, symlinks/hard links/FIFOs, read-only files, concurrent saves, external changes/replacements, authentication, same-origin/header enforcement, malformed/oversized JSON and encoded external URLs. Full Go tests, go vet, the installer regression and the ARM64 release build pass. A fresh focused reviewer found no Critical or Important issues; its special-permission-bit caveat is explicitly documented. Race testing remains unsupported on this Nano's 39-bit VMA.

The new real-browser suite first failed because Edit actions were absent, then passed all six checks on the deployed binary: repeated saves on a Unicode path; cancel/discard warnings; external conflicts preserving both on-disk and typed contents; binary/large-file errors; exact encoded SFTP metadata; and 390px dialogs. All eight existing browser checks pass as well. Reports are build/editor-check.json and build/browser-check.json. Fixtures were removed after both suites.

The installed binary matches the final build and the service restarted successfully, retaining /home/shai, existing HTTPS and authentication. External edit shows host 192.168.68.107, port 22, SSH username shai and a full file path/SFTP link. The SSH service is active and its configured SFTP subsystem was exercised locally through the installed sftp client. Opening a registered app, authenticating and saving from another physical computer require that computer's SFTP client and existing SSH credentials; no remote desktop-app setup or end-to-end client login is claimed.

## File and directory creation/deletion — 2026-10-24

At the owner's request, added New file, New folder and per-entry Delete actions. New files are empty, private, and open in the text editor; new folders are private and created in the current directory. Existing names are never overwritten. Hidden creations follow the existing display filter, with a success message explaining how to show them. File deletion requires explicit confirmation; directory deletion requires typing its exact name and permanently removes the subtree, including hidden entries. Root and synthetic parent navigation cannot be deleted. Removing symbolic or hard links affects the entry itself, leaving other link targets intact.

Filesystem tests first failed for missing APIs, then passed for Unicode names, private modes, collision protection, recursive deletion, hidden contents, root/traversal/escaping symlinks, dangling symlinks, hard links, invalid names/kinds, changed/replaced confirmations and parent-directory permissions. A stale-confirmation test exposed the need to include file size in metadata revisions on this device's coarse timestamp filesystem; adding size made the test pass. Mutations share the editor's write mutex, pinned parent roots, JSON limits and origin/custom-header protections. HTTP tests first failed for missing routes, then passed for create/preview/delete responses, duplicate/conflict errors, wrong or absent name/revision, root rejection, authentication, malformed/extra/oversized bodies and cross-origin/form requests.

A fresh focused review found no important backend issues, and identified a modern-browser pattern compatibility issue. A failing browser regression reproduced a nested path being accepted when native validation was bypassed; an explicit single-name check and escaped slash in the HTML pattern fixed it. The full management browser suite now passes 8/8, covering single-name validation, Unicode creation, automatic editor opening/saving, collisions/cancellation, confirmed deletion, typed recursive deletion without following an escaping symlink, stale target rejection, hidden creation/parent protection and 390px layout. The editor suite passes 6/6 and original dashboard suite 8/8: 22 browser checks total on the final installed binary. Go tests, go vet, the Python installer regression and ARM64 build also pass. The previously documented race-detector platform limitation remains.

The installed binary matches the final build, current /home/shai root and protected TLS/auth configuration are retained, and the service restarted successfully. The prior installed binary is retained under ignored build/ for rollback. All destructive checks used disposable test directories and their fixtures were removed. Permanent removal has no undo; external filesystem changes during the final revision-check/remove interval remain best-effort as documented for editing.

## Collapsible panels — 2026-10-24

At the owner's request, converted the four metric cards and System details, Storage, Network and Device files to native details/summary controls. All eight start open, collapse independently to labelled headings, and support Enter/Space. File controls are inside the collapsible content, and grid alignment permits each card to shrink independently. No application JavaScript or backend behavior changed; hidden panels keep receiving current readings and preserve their file-browser state. Disclosure state resets on page load.

The new browser test first failed because the eight disclosure controls were absent. On the deployed binary all five new checks pass: independent shrink/restore with visible headings; native keyboard interaction; collapsed file controls excluded from hit-testing/Tab navigation and still usable after expanding; polling without reopening panels; and 390px layout. The collapsed mobile screenshot was visually inspected. All existing management8/editor6/dashboard8 checks pass again, for 27 browser checks total. Fresh Go tests, go vet, the installer regression and ARM64 build pass. A focused independent read-only review found no concrete regressions.

The final installed binary matches the build, existing configuration is preserved, and the user service restarted successfully. The previous installed binary is retained at build/jetson-dashboard.before-collapsible-panels. Browser fixtures were cleaned up; the disposable test browser was closed.

## Updated login credentials — 2026-10-25

The owner changed both username and password in the private credentials file and requested restart. The original application fixed the username to dashboard and consumed a separate environment password digest, so restart alone could not apply both changes. Added DASHBOARD_USERNAME with the existing dashboard default, retained the constant-time password comparison, and privately synchronized username/password digest into the protected installed environment file. The installer now preserves a configured username during updates, and verification scripts use the username from their private credentials input. No credential values are included in this record.

TestConfiguredUsername first failed for the missing field, then passed for custom username, rejection of old/wrong logins, environment loading/default and invalid names. Fresh complete Go tests, go vet, installer unittest and native ARM64 build pass. The final binary was installed with backup, the service restarted active, and a certificate-verified request using the updated credentials returned health200. Missing or wrong credentials and the old default username returned401. README explains that the environment username/digest must be synchronized when editing the private credentials file.

A focused independent read-only source review found no important regression. Installed binary identity and protected configuration synchronization/permissions were verified again without printing values.

## ngrok upstream correction — 2026-10-25

A remote request failed because the manually launched agent targeted http://localhost:8443 while the dashboard listened on HTTPS at 192.168.68.107:8443. Both the host and scheme were wrong. The installed agent's local endpoint-update operation returned405, so the original matching command-line process was replaced with the enabled user service ngrok-dashboard.service. It retains the existing public URL, targets the actual HTTPS listener, verifies the upstream using the dashboard certificate, disables request inspection, and restarts on failure. The account token remains private in the existing ngrok configuration. No application-code or router changes were required.

Public HTTPS requests to https://mop-unsoiled-underdog.ngrok-free.dev return the dashboard and authenticated health200; unauthenticated health returns401. Through the public endpoint, an Origin-bearing JSON create request returned201, text read/save succeeded, and confirmed deletion returned200. On-disk fixture contents matched the saved text, and disposable fixtures were removed. Both services are active; the tunnel unit is enabled for startup. Remote desktop SFTP is separate from this web-only tunnel.
