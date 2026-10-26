# File editing

User authorization: “Okay, please do that, minimal edit option and external edit”, accepting the browser-editor proposal in the conversation.

Add Edit and External edit actions to regular files. The browser editor is a native dialog with a plain textarea, Save, Cancel, status/errors, and unsaved-change warnings. Accept existing UTF-8 text files up to 1 MiB, reject binary data, symlinks, hard links and special files. Keep the configured home-directory boundary and HTTP authentication. Save through a temporary file in the pinned parent directory followed by atomic rename, preserving owner/group and permission bits. A content/metadata revision rejects stale edits; concurrent saves from this application are serialized. External processes do not participate in the lock, so the final pre-rename check is best-effort for that narrow race. Preserve CRLF for files originally using CRLF exclusively.

External edit shows the full remote path and SFTP URL, with a copy button and optional registered SFTP-app link. Use the existing SSH service on port 22 and the export owner's SSH username; SSH login is separate from dashboard credentials. No desktop app is installed or selected by the dashboard.

Approaches: the user selected a minimal built-in editor plus external editing. A full browser IDE adds dependencies and is unnecessary. External editing alone would not provide in-browser edits. Use Go and native browser controls; no new libraries, syntax highlighting, create/delete/rename actions or autosave.
