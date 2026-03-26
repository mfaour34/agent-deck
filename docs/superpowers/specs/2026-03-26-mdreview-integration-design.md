# mdreview Integration Design

## Overview

Integrate [mdreview](https://github.com/mfaour34/mdreview) — a Python TUI for reviewing markdown documents with inline comments — as a first-class feature in agent-deck. Users press a keybinding on any session to open a file picker, select `.md` files from the project, and launch mdreview as an ephemeral review session.

## Motivation

Agent-deck manages AI coding sessions that produce markdown artifacts (specs, plans, design docs). Currently there is no way to review these documents from within agent-deck. mdreview provides inline comments, review decisions (approved/changes requested), and CI-friendly exit codes — a natural fit for the workflow.

## Design

### 1. File Picker Dialog

A new `ReviewFileDialog` TUI component following the pattern of existing dialogs (`mcp_dialog.go`, `skill_dialog.go`).

**Trigger:** Press `p` on a selected session in the home view.

**Behavior:**
- Recursively scans the session's project path for `.md` files. Path resolution order: `WorktreePath` if non-empty, otherwise `ProjectPath`. Skips `.git`, `node_modules`, `vendor`, and symlinks to avoid loops and noise.
- Displays relative paths sorted alphabetically in a scrollable list
- Multi-select via `space` to toggle individual files
- `a` to toggle select all/none
- Type to filter the list (jump-to-name, like MCP dialog)
- `j/k` or arrow keys to navigate
- `enter` to confirm selection, `esc` to cancel
- Counter at top showing "N selected"

**File:** `internal/ui/review_dialog.go` (new)

### 2. Ephemeral Session Lifecycle

**Session creation:**
- After file selection, creates a new session with `Tool: "mdreview"`
- Command: `mdreview <file1> <file2> ...` (absolute paths)
- Working directory: source session's project path
- Title: `"review: <source-session-name>"`
- Group: same group as the source session
- Session is created and selected but NOT auto-attached

**New status — `StatusCompleted`:**
- Added to the `Status` enum in `instance.go`: `StatusCompleted Status = "completed"`
- Completed sessions display with a dimmed style and checkmark icon in the session list (use `ColorComment` foreground, consistent with inactive styling)
- Completed sessions persist until manually deleted by the user
- `StatusCompleted` sessions skip the error-recheck optimization in `UpdateStatus()` (they are terminal, not errors)

**Process exit detection:**
- In `UpdateStatus()` (`instance.go` around line 2097), the tmux status detection switch handles `"inactive"` (process exited, pane dead) which currently maps to `StatusError`
- Add a check: when `Tool == "mdreview"` and the detected state is `"inactive"`, set `StatusCompleted` instead of `StatusError`
- This is a targeted two-line check in the existing switch, not a generic concept

**Re-attach behavior for completed sessions:**
- When user presses Enter on a `StatusCompleted` session, the tmux session is dead
- In the attach handler in `home.go`, detect `StatusCompleted` and instead of attaching, restart the session: call `inst.Start()` to recreate the tmux session with the stored `Command`, then attach
- Reset status to `StatusRunning` before restarting
- The stored `Command` field on the Instance already contains the full `mdreview file1 file2 ...` invocation, so no additional state is needed
- Store selected files as `ReviewFiles []string` on the Instance (JSON field) for reliable reconstruction if paths contain spaces

### 3. Installation Check

**First-use flow:**
1. When `p` is pressed, check `exec.LookPath("mdreview")`
2. If found: proceed to file picker
3. If missing: show confirmation dialog — "mdreview is not installed. Install via pipx from github.com/mfaour34/mdreview?"
4. If user confirms: run `pipx install git+https://github.com/mfaour34/mdreview`
5. If pipx is not found: show error "pipx is required to install mdreview. Install pipx first: `brew install pipx`" — do NOT fall back to bare `pip install` (PEP 668 on modern Python will reject it)
6. Installation runs as a `tea.Cmd` (async goroutine returning a message on completion) so the TUI remains responsive during the 10-30 second install
7. On completion, show success/error in the status bar area
8. Cache successful LookPath check in an in-memory flag (skip re-checking for rest of agent-deck session)

### 4. Help Bar & Keybinding

**Hotkey registration:**
- New unexported constant: `hotkeyReview = "review"` in `hotkeys.go` (follows existing pattern of `hotkeyXxx`)
- Default binding: `"p"` added to `defaultHotkeyBindings` map
- Add to `hotkeyActionOrder` slice for lookup builder
- Rebindable via user config

**Help bar entries (session context only):**
- Full (100+ cols): `[p]Review` in secondary actions
- Compact (70-99 cols): `[p]Rev`
- Minimal (50-69 cols): `[p]Rev`
- Tiny (<50 cols): omitted

**Activation condition:** Only active when cursor is on a session (not group header, not empty list). Disabled when the selected session has `Tool == "mdreview"` (no reviewing a review session) or is SSH/remote.

## File Changes

| File | Action | What |
|------|--------|------|
| `internal/ui/review_dialog.go` | Create | ReviewFileDialog component |
| `internal/session/instance.go` | Modify | Add `StatusCompleted` to Status enum, `ReviewFiles` field, UpdateStatus check |
| `internal/ui/home.go` | Modify | Wire `p` key, handle dialog, create review session, install check, help bar, re-attach logic |
| `internal/ui/hotkeys.go` | Modify | Register `hotkeyReview` constant, default binding, action order |
| `internal/ui/review_dialog_test.go` | Create | Tests for ReviewFileDialog |

## Data Flow

```
User presses 'p' on session
  -> Check mdreview installed (exec.LookPath)
  -> If missing: confirm dialog -> install via pipx/pip
  -> Scan ProjectPath for .md files (filepath.WalkDir)
  -> Show ReviewFileDialog with file list
  -> User multi-selects files, presses Enter
  -> Create new session: Tool="mdreview", Command="mdreview file1 file2 ..."
  -> Session appears in same group, status=running
  -> User attaches when ready (Enter)
  -> When mdreview exits -> status=completed
  -> Session stays in list until manually deleted
```

## Edge Cases

- **No `.md` files found:** Show message "No markdown files found in project path" and dismiss
- **Session has no project path:** Disable the keybinding (don't show in help bar)
- **mdreview install fails:** Show error in status bar, don't proceed to file picker
- **User selects 0 files and presses Enter:** Validation prevents — require at least 1 file
- **Re-attach completed session:** Restart tmux session with stored command, reset to running
- **Source session is SSH/remote:** Disable for now (mdreview needs local filesystem access)
- **Pressing `p` on a review session:** Disabled (no reviewing a review)
- **File paths with spaces:** `ReviewFiles []string` field stores paths; command reconstructed from field, not parsed from string

## Out of Scope

- Porting mdreview logic to Go (too complex, Python TUI works fine)
- Custom mdreview configuration from agent-deck (users configure via `~/.config/mdreview/keys.toml` directly)
- Automatic review triggering (e.g., after plan generation) — future enhancement
- Review status reporting back to agent-deck (e.g., approved/rejected badge) — future enhancement
