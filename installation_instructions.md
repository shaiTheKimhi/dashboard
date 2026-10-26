# Installation instructions for Codex

Copy the following prompt into Codex on the Linux server where you want to install the dashboard. It installs the version currently on GitHub and adapts the original Jetson's machine-specific settings to the new server.

```text
Install and run this dashboard on this Linux server:
https://github.com/shaiTheKimhi/dashboard

Complete the installation and verify it works.

Requirements:
- Prefer installation as my normal user, without sudo.
- Use this server's username, home directory, IP and CPU architecture.
- Do not copy the original Jetson's credentials, IP, ngrok URL or ARM64 build settings.
- Default the shared directory to $HOME/dashboard-share unless I specify another directory.
- Preserve existing installations, credentials and certificates when updating.
- Keep passwords, tokens and private keys out of chat and logs.

1. Inspect this server
Determine the Linux distribution, architecture, current user, available
private IPv4 addresses, and whether systemd user services work.
Check for Git, Python 3, OpenSSL and Go.
Read the repository's README and applicable AGENTS.md instructions.

2. Obtain the project
Clone:
  git clone https://github.com/shaiTheKimhi/dashboard.git

If a checkout already exists, inspect it and preserve local changes.
Use the repository's default branch.

3. Prepare Go
The application requires Go 1.25 or newer.
Use a supported, patched release. If necessary, install an official
Go toolchain under my home directory and verify its published SHA-256.
Do not replace system toolchains.
No Node/npm, database, container or application dependencies are needed.

4. Test and build
From the repository, run:
  go test ./...
  go vet ./...
  python3 -m unittest discover -s deploy -p 'test_*.py'

Build a native Linux binary for THIS server:
  mkdir -p build
  env -u GOOS -u GOARCH -u GOARM64 CGO_ENABLED=0 \
    go build -trimpath -o build/jetson-dashboard .

A non-Jetson server may show unavailable GPU/Jetson sensors; that is
expected and must not prevent normal monitoring or file management.

5. Install
For a server with a suitable private IPv4 address, run:
  python3 deploy/install.py --user \
    --ip <THIS_SERVER_PRIVATE_IPV4> \
    --file-root "$HOME/dashboard-share"

Always supply --file-root: the installer's default references the
original machine.

The installer rejects the entire home directory as an export. If I
explicitly request home-directory browsing, install with the dedicated
folder first, then set DASHBOARD_FILE_ROOT to my actual home directory
in the installed environment file and restart.

If there is no suitable private IPv4, do not invent one. Configure an
equivalent user installation on HTTPS at 127.0.0.1:8443, with fresh
credentials and a certificate covering 127.0.0.1. Use SSH forwarding or
the optional ngrok setup for access.

6. Service and credentials
Ensure jetson-dashboard.service is running and enabled as a user service.
Check linger before claiming it will start before login. If enabling
linger requires an administrator, report the exact remaining command.

Keep configuration under:
  ~/.config/jetson-dashboard/

Generate fresh credentials and tell me their file location.
The active username and password hash live in dashboard.env;
credentials.txt alone is not read by the server.

Explain how to trust the self-signed certificate for direct HTTPS access.

7. Optional ngrok access
If I request remote access, install the native ngrok binary without sudo.
Use this server's own endpoint; do not reuse or disconnect the original
Jetson's endpoint.
Have me configure any missing account token privately.

Forward to the dashboard's exact HTTPS listener, not a bare port.
Enable upstream certificate verification using its certificate file,
disable request inspection, and run ngrok as a user service.

8. Verify and report
Verify:
- Both the page and authenticated /healthz work.
- Unauthenticated requests return 401.
- Metrics and directory browsing work.
- Create/edit/delete work using disposable test files only.
- If ngrok is configured, authenticated access and file mutations work
  through the public URL as well.

Finish with the working address, shared directory, credentials-file
location, service restart/status commands, and any remaining limitations.
```
