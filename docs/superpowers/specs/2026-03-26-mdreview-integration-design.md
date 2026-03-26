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
- Recursively scans the session's project path (or `EffectiveWorkingDir` for multi-repo) for `.md` files
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
- Added to the `Status` enum in `instance.go`
- When mdreview exits (any exit code: 0=approved, 1=changes requested, 2=incomplete), the session transitions to `StatusCompleted` instead of idle/error
- Completed sessions display a distinct visual indicator (dimmed or checkmark icon) in the session list
- Completed sessions persist until manually deleted by the user
- Re-attaching a completed session relaunches mdreview on the same files

**Process exit detection:**
- Agent-deck already monitors tmux panes for process status
- When `Tool == "mdreview"` and the process exits cleanly, set `StatusCompleted`
- This is a targeted check in the existing status detection logic, not a generic "completed" concept for all tools

### 3. Installation Check

**First-use flow:**
1. When `p` is pressed, check `exec.LookPath("mdreview")`
2. If found: proceed to file picker
3. If missing: show confirmation dialog — "mdreview is not installed. Install from github.com/mfaour34/mdreview? (requires pipx)"
4. If user confirms: run `pipx install git+https://github.com/mfaour34/mdreview`
5. If pipx is missing: fall back to `pip install git+https://github.com/mfaour34/mdreview`
6. Show status message during install ("Installing mdreview...")
7. Cache successful check in memory (skip re-checking for rest of session)

### 4. Help Bar & Keybinding

**Hotkey registration:**
- New constant: `HotkeyReview = "review"` in `hotkeys.go`
- Default binding: `"p"`
- Rebindable via user config

**Help bar entries (session context only):**
- Full (100+ cols): `[p]Review` in secondary actions
- Compact (70-99 cols): `[p]Rev`
- Minimal (50-69 cols): `[p]Rev`
- Tiny (<50 cols): omitted

**Activation condition:** Only active when cursor is on a session (not group header, not empty list).

## File Changes

| File | Action | What |
|------|--------|------|
| `internal/ui/review_dialog.go` | Create | ReviewFileDialog component |
| `internal/session/instance.go` | Modify | Add `StatusCompleted` to Status enum |
| `internal/ui/home.go` | Modify | Wire `p` key, handle dialog, create review session, install check, help bar |
| `internal/ui/hotkeys.go` | Modify | Register `HotkeyReview` constant and default binding |

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
- **Re-attach completed session:** Relaunch mdreview with the stored command (same files)
- **Source session is SSH/remote:** Disable for now (mdreview needs local filesystem access)

## Out of Scope

- Porting mdreview logic to Go (too complex, Python TUI works fine)
- Custom mdreview configuration from agent-deck (users configure via `~/.config/mdreview/keys.toml` directly)
- Automatic review triggering (e.g., after plan generation) — future enhancement
- Review status reporting back to agent-deck (e.g., approved/rejected badge) — future enhancement
