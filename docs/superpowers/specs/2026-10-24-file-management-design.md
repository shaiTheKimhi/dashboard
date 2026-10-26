# Create and delete files and directories

Requested by the owner: add/delete files and directories from the existing dashboard. Extend the existing authenticated file browser, native dialogs and rooted filesystem operations; no dependencies or Git initialization.

New file/New folder actions create a single named entry in the current folder with private permissions, refuse existing entries, and refresh the list. New empty files open in the existing text editor. Names may contain spaces/Unicode and dots; reject empty names, dot/dot-dot, slash, controls, invalid UTF-8 and names over 255 bytes. Hidden creations remain covered by the hidden-file filter.

Delete appears on listed files/directories/links, never on the synthetic parent row or root. Load a confirmation preview with a metadata revision. Files require explicit confirmation. Directories require typing their exact name and delete recursively, including hidden contents. Symlinks are removed themselves, not their targets; hard-link removal likewise removes only that entry. Root cannot be deleted. Parent roots are pinned; all actions stay within the configured tree. Mutations share the existing save lock and the same JSON/custom-header/origin protections. Stale confirmation revisions return conflict; concurrent external filesystem changes have the same final-check race limitation as editing.

Alternatives: reusing native browser dialogs fits the existing minimal UI. A separate file-manager app adds setup and would not provide the requested dashboard actions. An empty-directory-only delete would leave ordinary nonempty folders unsupported, so use recursive deletion with explicit folder-name confirmation. No upload, move/rename, recycle bin or bulk selection in this change.
