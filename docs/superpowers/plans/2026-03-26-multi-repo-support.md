# Multi-Repo Support + Path Completion Fix

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port multi-repo support from upstream (asheshgoplani/agent-deck) into the fork, allowing sessions to access multiple repos, while keeping epic runner intact. Also fix path tab-completion behavior.

**Architecture:** Multi-repo adds fields to `Instance` (`MultiRepoEnabled`, `AdditionalPaths`, `MultiRepoTempDir`, `MultiRepoWorktrees`). When enabled, a temp directory with symlinks (or worktrees) is created as the session's working directory. Each additional repo is passed to Claude via `--add-dir` flags. The new session dialog gets a `focusMultiRepo` target with a stacked path list UI (add/remove/edit). Path tab-completion is fixed by returning exact directory matches instead of listing children.

**Tech Stack:** Go, Bubble Tea (TUI), tmux, git worktrees

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/session/instance.go` | Modify | Add multi-repo struct fields, helper methods, `--add-dir` generation |
| `internal/session/utils.go` | Modify | Fix `GetDirectoryCompletions` for exact dir matches, remove `LongestCommonPrefix` |
| `internal/ui/newdialog.go` | Modify | Add multi-repo toggle, path list UI, editing, validation |
| `internal/ui/home.go` | Modify | Wire multi-repo into session creation, deletion cleanup, fork propagation |

---

### Task 1: Fix Path Tab-Completion in `utils.go`

**Files:**
- Modify: `internal/session/utils.go:58-62` (GetDirectoryCompletions exact dir match)
- Modify: `internal/session/utils.go:98-117` (remove LongestCommonPrefix)
- Test: `internal/session/utils_test.go`

- [ ] **Step 1: Update GetDirectoryCompletions — exact dir match returns itself**

In `internal/session/utils.go`, replace the exact-directory branch (lines 58-62):

```go
// BEFORE (lines 58-62):
} else if info, err := os.Stat(input); err == nil && info.IsDir() {
    // Exact directory match without trailing slash — list children so the
    // user can drill deeper on the next Tab press.
    dir = input
    prefix = ""

// AFTER:
} else if info, err := os.Stat(input); err == nil && info.IsDir() {
    // Exact directory match without trailing slash - return itself
    return []string{originalInput}, nil
```

- [ ] **Step 2: Remove LongestCommonPrefix function**

Delete the `LongestCommonPrefix` function (lines 98-117 of `internal/session/utils.go`).

- [ ] **Step 3: Run existing tests**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go test ./internal/session/ -run TestGetDirectoryCompletions -v`
Expected: PASS (existing tests should still pass; if any test relied on the old behavior of listing children for exact dirs, update the expectation)

- [ ] **Step 4: Commit**

```bash
git add internal/session/utils.go internal/session/utils_test.go
git commit -m "fix: path tab-completion returns exact dir match instead of listing children"
```

---

### Task 2: Update Tab-Completion UI in `newdialog.go`

**Files:**
- Modify: `internal/ui/newdialog.go` (remove `tabCompletions`/`tabCompletionIdx` fields and related code, simplify tab handler)

This task removes the `tabCompletions` dropdown approach that was interleaved with path suggestions, since the new `GetDirectoryCompletions` behavior makes it unnecessary. The cycler still works for multiple matches.

- [ ] **Step 1: Remove `tabCompletions` and `tabCompletionIdx` fields**

In `internal/ui/newdialog.go`, remove from `NewDialog` struct (lines 78-79):
```go
// REMOVE these two lines:
tabCompletions   []string                // filesystem completions shown in dropdown.
tabCompletionIdx int                     // selected index in tabCompletions dropdown.
```

- [ ] **Step 2: Simplify tab handler in Update method**

Replace the tab handling block (lines 819-871) with a simpler version that just uses the cycler:

```go
case "tab":
    // On path field: smart filesystem autocomplete.
    if cur == focusPath {
        path := d.pathInput.Value()

        // If the cycler is already active, cycle to next match.
        if d.pathCycler.IsActive() {
            val := d.pathCycler.Next()
            d.pathInput.SetValue(val)
            d.pathInput.SetCursor(len(val))
            return d, nil
        }

        // Get filesystem completions for the current input.
        matches, err := session.GetDirectoryCompletions(path)
        if err == nil && len(matches) > 0 {
            if len(matches) == 1 {
                // Single match — complete and append / so next Tab drills in.
                completed := matches[0] + string(os.PathSeparator)
                d.pathInput.SetValue(completed)
                d.pathInput.SetCursor(len(completed))
                d.pathCycler.Reset()
            } else {
                // Multiple matches — start cycling.
                d.pathCycler.SetMatches(matches)
                val := d.pathCycler.Next()
                d.pathInput.SetValue(val)
                d.pathInput.SetCursor(len(val))
            }
            return d, nil
        }
    }
```

- [ ] **Step 3: Remove all remaining `tabCompletions` references**

Search for and remove:
- `d.tabCompletions = nil` (lines 221, 800, 808, 847, 1074)
- The tab-completions dropdown rendering in View (lines 1302-1356)
- Any `d.tabCompletionIdx` references

- [ ] **Step 4: Remove `LongestCommonPrefix` call**

The call at line 851 (`lcp := session.LongestCommonPrefix(matches)`) is no longer needed since we simplified the tab handler.

- [ ] **Step 5: Build and verify**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build with no errors

- [ ] **Step 6: Commit**

```bash
git add internal/ui/newdialog.go
git commit -m "refactor: simplify path tab-completion, remove tabCompletions dropdown"
```

---

### Task 3: Add Multi-Repo Fields to `Instance`

**Files:**
- Modify: `internal/session/instance.go:80-81` (add struct fields after EpicID)
- Modify: `internal/session/instance.go:193` (add MultiRepoWorktree type after SandboxConfig)
- Modify: `internal/session/instance.go:195-198` (add helper methods after IsSandboxed)

- [ ] **Step 1: Add multi-repo fields to Instance struct**

In `internal/session/instance.go`, after line 80 (after `EpicID` field), add:

```go
	// Multi-repo support
	MultiRepoEnabled   bool                `json:"multi_repo_enabled,omitempty"`
	AdditionalPaths    []string            `json:"additional_paths,omitempty"`    // Paths beyond ProjectPath
	MultiRepoTempDir   string              `json:"multi_repo_temp_dir,omitempty"` // Temp cwd for multi-repo sessions
	MultiRepoWorktrees []MultiRepoWorktree `json:"multi_repo_worktrees,omitempty"`
```

- [ ] **Step 2: Add MultiRepoWorktree type and helper functions**

After `SandboxConfig` struct (after line 193), add:

```go
// resolveRealPath resolves symlinks to get the canonical path for comparison.
// Falls back to the original path on error (e.g., path doesn't exist yet).
func resolveRealPath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// DeduplicateDirnames returns unique directory names for the given paths.
// When multiple paths share the same basename, a numeric suffix is appended (e.g., "src-1").
func DeduplicateDirnames(paths []string) []string {
	seen := make(map[string]int)
	result := make([]string, len(paths))
	for i, p := range paths {
		dirname := filepath.Base(p)
		if n := seen[dirname]; n > 0 {
			result[i] = fmt.Sprintf("%s-%d", dirname, n)
		} else {
			result[i] = dirname
		}
		seen[dirname]++
	}
	return result
}

// MultiRepoWorktree tracks a worktree created for one repo in a multi-repo session.
type MultiRepoWorktree struct {
	OriginalPath string `json:"original_path"`
	WorktreePath string `json:"worktree_path"`
	RepoRoot     string `json:"repo_root"`
	Branch       string `json:"branch"`
}
```

- [ ] **Step 3: Add Instance helper methods**

After the new types, add:

```go
// IsMultiRepo returns true if this session has multi-repo mode enabled.
func (inst *Instance) IsMultiRepo() bool {
	return inst.MultiRepoEnabled
}

// AllProjectPaths returns all project paths: [ProjectPath] + AdditionalPaths.
func (inst *Instance) AllProjectPaths() []string {
	paths := []string{inst.ProjectPath}
	paths = append(paths, inst.AdditionalPaths...)
	return paths
}

// EffectiveWorkingDir returns the working directory for this session.
// For multi-repo sessions, this is the temp dir; otherwise the ProjectPath.
func (inst *Instance) EffectiveWorkingDir() string {
	if inst.MultiRepoEnabled && inst.MultiRepoTempDir != "" {
		return inst.MultiRepoTempDir
	}
	return inst.ProjectPath
}

// CleanupMultiRepoTempDir removes the multi-repo temporary directory.
func (inst *Instance) CleanupMultiRepoTempDir() error {
	if inst.MultiRepoTempDir == "" {
		return nil
	}
	return os.RemoveAll(inst.MultiRepoTempDir)
}
```

- [ ] **Step 4: Add `--add-dir` flags for multi-repo in buildClaudeExtraFlags**

In `buildClaudeExtraFlags` (line 536, after the `ParentProjectPath` block), add:

```go
	// Multi-repo: pass all project paths via --add-dir (deduplicated, excluding cwd)
	if i.MultiRepoEnabled {
		seen := make(map[string]bool)
		if i.ParentProjectPath != "" {
			seen[resolveRealPath(i.ParentProjectPath)] = true // already added above
		}
		seen[resolveRealPath(i.EffectiveWorkingDir())] = true // exclude cwd
		for _, p := range i.AllProjectPaths() {
			real := resolveRealPath(p)
			if seen[real] {
				continue
			}
			seen[real] = true
			flags = append(flags, fmt.Sprintf("--add-dir %s", p))
		}
	}
```

- [ ] **Step 5: Build and verify**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 6: Commit**

```bash
git add internal/session/instance.go
git commit -m "feat: add multi-repo fields and helper methods to Instance"
```

---

### Task 4: Add Multi-Repo Toggle and Path List UI to NewDialog

**Files:**
- Modify: `internal/ui/newdialog.go` (add focusMultiRepo, multi-repo fields, toggle, path list editing, view rendering)

This is the largest task. It adds the multi-repo toggle and stacked path list to the new session dialog.

- [ ] **Step 1: Add `focusMultiRepo` to focus targets**

In the `focusTarget` const block (line 21-32), add after `focusSandbox` (line 26):

```go
const (
	focusName        focusTarget = iota
	focusPath                    // project path input.
	focusCommand                 // tool/command picker.
	focusWorktree                // worktree checkbox.
	focusSandbox                 // sandbox checkbox.
	focusMultiRepo               // multi-repo toggle (transforms path into list when enabled).
	focusEpicRunner              // epic runner checkbox.
	focusEpicID                  // epic ID input (conditional — only when epic runner enabled).
	focusInherited               // inherited Docker settings toggle (conditional).
	focusBranch                  // branch input (conditional — only when worktree enabled).
	focusOptions                 // tool-specific options panel (conditional).
)
```

- [ ] **Step 2: Add multi-repo fields to NewDialog struct**

After `pathCycler` field (line 77), add:

```go
	// Multi-repo mode.
	multiRepoEnabled    bool
	multiRepoPaths      []string // All paths when multi-repo is active.
	multiRepoPathCursor int      // Selected path index in the stacked list.
	multiRepoEditing    bool     // True when editing a path entry.
```

- [ ] **Step 3: Add multi-repo fields to dialogSnapshot**

In `dialogSnapshot` struct (after `codexYolo` field), add:

```go
	multiRepoEnabled bool
	multiRepoPaths   []string
```

- [ ] **Step 4: Add `path/filepath` import**

Add `"path/filepath"` to the imports at the top of `newdialog.go` if not already present.

- [ ] **Step 5: Update saveSnapshot and restoreSnapshot**

In `saveSnapshot()`, add to the returned struct:
```go
	multiRepoEnabled: d.multiRepoEnabled,
	multiRepoPaths:   append([]string{}, d.multiRepoPaths...),
```

In `restoreSnapshot()`, add after existing restores:
```go
	d.multiRepoEnabled = s.multiRepoEnabled
	d.multiRepoPaths = append([]string{}, s.multiRepoPaths...)
	d.multiRepoPathCursor = 0
	d.multiRepoEditing = false
```

- [ ] **Step 6: Reset multi-repo in ShowInGroup and previewRecentSession**

In `ShowInGroup()`, after the epic runner reset (after line 241), add:
```go
	// Reset multi-repo.
	d.multiRepoEnabled = false
	d.multiRepoPaths = nil
	d.multiRepoPathCursor = 0
	d.multiRepoEditing = false
```

In `previewRecentSession()`, add similar reset after worktree reset.

- [ ] **Step 7: Add ToggleMultiRepo, GetMultiRepoPaths, IsMultiRepoEditing methods**

After the existing `ToggleEpicRunner` method, add:

```go
// ToggleMultiRepo toggles multi-repo mode.
// When enabling, initializes multiRepoPaths with the current pathInput value.
// When disabling, collapses back to the first path.
func (d *NewDialog) ToggleMultiRepo() {
	d.multiRepoEnabled = !d.multiRepoEnabled
	if d.multiRepoEnabled {
		currentPath := strings.TrimSpace(d.pathInput.Value())
		if currentPath != "" {
			d.multiRepoPaths = []string{currentPath}
		} else {
			d.multiRepoPaths = []string{""}
		}
		d.multiRepoPathCursor = 0
		d.multiRepoEditing = false
	} else {
		// Collapse back to the first non-empty path
		if len(d.multiRepoPaths) > 0 {
			d.pathInput.SetValue(d.multiRepoPaths[0])
		}
		d.multiRepoPaths = nil
		d.multiRepoPathCursor = 0
		d.multiRepoEditing = false
	}
	d.rebuildFocusTargets()
}

// GetMultiRepoPaths returns the multi-repo paths and enabled state.
func (d *NewDialog) GetMultiRepoPaths() ([]string, bool) {
	if !d.multiRepoEnabled {
		return nil, false
	}
	var paths []string
	for _, p := range d.multiRepoPaths {
		p = strings.TrimSpace(p)
		if p != "" {
			p = strings.Trim(p, "'\"")
			if idx := strings.Index(p, "~/"); idx > 0 {
				p = p[idx:]
			}
			p = session.ExpandPath(p)
			paths = append(paths, p)
		}
	}
	return paths, true
}

// IsMultiRepoEditing returns true when the user is editing a path in the multi-repo list.
// Used by the parent to prevent enter from submitting the form.
func (d *NewDialog) IsMultiRepoEditing() bool {
	return d.multiRepoEnabled && d.currentTarget() == focusMultiRepo
}
```

- [ ] **Step 8: Add isTextInputFocused method**

Add before the `Update` method:

```go
// isTextInputFocused returns true when a text input field is actively receiving
// keystrokes. Single-letter shortcuts must be suppressed in this state.
func (d *NewDialog) isTextInputFocused() bool {
	switch d.currentTarget() {
	case focusName, focusPath, focusBranch:
		return true
	case focusCommand:
		return d.commandCursor == 0 // custom command input
	case focusEpicID:
		return true
	case focusMultiRepo:
		return d.multiRepoEditing
	default:
		return false
	}
}
```

- [ ] **Step 9: Update rebuildFocusTargets**

Replace the targets initialization in `rebuildFocusTargets()` (line 658):

```go
func (d *NewDialog) rebuildFocusTargets() {
	var targets []focusTarget
	if d.multiRepoEnabled {
		// Multi-repo replaces the single path field with a path list under focusMultiRepo
		targets = []focusTarget{focusName, focusMultiRepo, focusCommand, focusWorktree, focusSandbox, focusEpicRunner}
	} else {
		// Toggle row appears AFTER path so the user fills in path first
		targets = []focusTarget{focusName, focusPath, focusMultiRepo, focusCommand, focusWorktree, focusSandbox, focusEpicRunner}
	}
	if d.epicRunnerEnabled {
		targets = append(targets, focusEpicID)
	}
	// ... rest unchanged (sandbox, worktree, options)
```

Note: `focusMultiRepo` appears in BOTH branches — when disabled it's the toggle row (after path), when enabled it becomes the path list (replacing the single path field).

- [ ] **Step 10: Update updateFocus for focusMultiRepo**

In `updateFocus()`, add a case in the switch:
```go
	case focusMultiRepo:
		// Toggle row or path list — no text input unless editing.
```

- [ ] **Step 11: Add multi-repo key handling in Update method**

Add `"m"` shortcut (near the `"e"` and `"s"` shortcuts, around line 994):
```go
	case "m":
		if cur == focusCommand && !d.isTextInputFocused() {
			d.ToggleMultiRepo()
			return d, nil
		}
```

Add space toggle for `focusMultiRepo` (in the `" "` case, around line 1018):
```go
		if cur == focusMultiRepo {
			d.ToggleMultiRepo()
			return d, nil
		}
```

Add multi-repo path navigation in `"down"` handler:
```go
	case "down":
		if cur == focusMultiRepo && d.multiRepoEnabled && !d.multiRepoEditing {
			if d.multiRepoPathCursor < len(d.multiRepoPaths)-1 {
				d.multiRepoPathCursor++
				return d, nil
			}
		}
```

Add in `"shift+tab"` / `"up"` handler:
```go
		if cur == focusMultiRepo && d.multiRepoEnabled && !d.multiRepoEditing {
			if d.multiRepoPathCursor > 0 {
				d.multiRepoPathCursor--
				return d, nil
			}
		}
```

Add Escape handler for multi-repo editing (CRITICAL: without this, Escape while editing closes the entire dialog):
```go
	case "esc":
		if d.multiRepoEditing {
			d.multiRepoEditing = false
			d.pathInput.Blur()
			d.pathCycler.Reset()
			return d, nil
		}
```

This must be placed BEFORE the existing Escape handler that hides the dialog.

Add enter handling for multi-repo editing:
```go
	case "enter":
		if cur == focusMultiRepo && d.multiRepoEnabled {
			if d.multiRepoEditing {
				// Save the edited path back
				d.multiRepoPaths[d.multiRepoPathCursor] = strings.TrimSpace(d.pathInput.Value())
				d.multiRepoEditing = false
				d.pathInput.Blur()
				d.pathCycler.Reset()
			} else {
				// Start editing: load path into pathInput
				d.multiRepoEditing = true
				d.pathInput.SetValue(d.multiRepoPaths[d.multiRepoPathCursor])
				d.pathInput.SetCursor(len(d.pathInput.Value()))
				d.pathInput.Focus()
				d.pathCycler.Reset()
				d.suggestionNavigated = false
				d.pathSuggestionCursor = 0
				d.filterPathSuggestions()
			}
			return d, nil
		}
```

Add `"a"` for adding paths and `"d"` for removing:
```go
	case "a":
		if cur == focusMultiRepo && d.multiRepoEnabled && !d.multiRepoEditing {
			defaultPath := ""
			for i := len(d.multiRepoPaths) - 1; i >= 0; i-- {
				if p := strings.TrimSpace(d.multiRepoPaths[i]); p != "" {
					defaultPath = filepath.Dir(session.ExpandPath(p))
					if defaultPath != "" && defaultPath != "." {
						if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(defaultPath, home) {
							defaultPath = "~" + defaultPath[len(home):]
						}
						defaultPath += string(os.PathSeparator)
					} else {
						defaultPath = ""
					}
					break
				}
			}
			d.multiRepoPaths = append(d.multiRepoPaths, defaultPath)
			d.multiRepoPathCursor = len(d.multiRepoPaths) - 1
			d.multiRepoEditing = true
			d.pathInput.SetValue(defaultPath)
			d.pathInput.SetCursor(len(defaultPath))
			d.pathInput.Focus()
			d.pathCycler.Reset()
			d.suggestionNavigated = false
			d.pathSuggestionCursor = 0
			d.filterPathSuggestions()
			return d, nil
		}

	case "d":
		if cur == focusMultiRepo && d.multiRepoEnabled && !d.multiRepoEditing && len(d.multiRepoPaths) > 1 {
			d.multiRepoPaths = append(d.multiRepoPaths[:d.multiRepoPathCursor], d.multiRepoPaths[d.multiRepoPathCursor+1:]...)
			if d.multiRepoPathCursor >= len(d.multiRepoPaths) {
				d.multiRepoPathCursor = len(d.multiRepoPaths) - 1
			}
			return d, nil
		}
```

- [ ] **Step 12: Extend tab handler for multi-repo editing**

Update the tab handler to also work when `d.multiRepoEditing`:
```go
	case "tab":
		isPathEditing := cur == focusPath || d.multiRepoEditing
		if isPathEditing {
			// ... same tab completion logic using pathInput ...
		}
```

- [ ] **Step 13: Update focused input handling**

In the "Update focused input" switch (around line 1060), add:
```go
	case focusMultiRepo:
		if d.multiRepoEditing {
			oldValue := d.pathInput.Value()
			d.pathInput, cmd = d.pathInput.Update(msg)
			if d.pathInput.Value() != oldValue {
				d.suggestionNavigated = false
				d.pathSuggestionCursor = 0
				d.pathCycler.Reset()
				d.filterPathSuggestions()
			}
		}
```

Also update `focusWorktree, focusSandbox, focusEpicRunner, focusInherited` case to not include `focusMultiRepo` (it has its own case now).

- [ ] **Step 14: Update Validate for multi-repo**

In `Validate()`, modify the empty path check to skip when multi-repo is enabled, then add multi-repo validation:
```go
	// Skip single-path empty check when multi-repo is enabled (paths come from multiRepoPaths)
	if path == "" && !d.multiRepoEnabled {
		return "Project path cannot be empty"
	}
```

Then add multi-repo validation:
```go
	if d.multiRepoEnabled {
		nonEmpty := 0
		seen := make(map[string]bool)
		for _, p := range d.multiRepoPaths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			expanded := session.ExpandPath(strings.Trim(p, "'\""))
			if seen[expanded] {
				return "Duplicate paths in multi-repo mode"
			}
			seen[expanded] = true
			nonEmpty++
		}
		if nonEmpty < 2 {
			return "Multi-repo mode requires at least 2 paths"
		}
	}
```

- [ ] **Step 15: Add multi-repo rendering in View method**

After the sandbox checkbox rendering (line 1428), before the epic runner checkbox, add:

```go
	// Multi-repo checkbox.
	multiRepoLabel := "Multi-repo mode"
	if cur == focusCommand {
		multiRepoLabel = "Multi-repo mode (m)"
	}
	content.WriteString(renderCheckboxLine(multiRepoLabel, d.multiRepoEnabled, cur == focusMultiRepo && !d.multiRepoEnabled))

	if d.multiRepoEnabled {
		// Multi-repo path list replaces the single path field.
		dimStyle := lipgloss.NewStyle().Foreground(ColorComment)
		pathFocused := cur == focusMultiRepo
		if pathFocused {
			content.WriteString(activeLabelStyle.Render("▶ Paths:"))
		} else {
			content.WriteString(labelStyle.Render("  Paths:"))
		}
		content.WriteString("\n")
		if pathFocused {
			for i, p := range d.multiRepoPaths {
				isSelected := i == d.multiRepoPathCursor
				prefix := "    "
				if isSelected {
					prefix = "  ▸ "
				}
				if isSelected && d.multiRepoEditing {
					content.WriteString(fmt.Sprintf("%s%d. ", prefix, i+1))
					content.WriteString(d.pathInput.View())
					content.WriteString("\n")
				} else {
					display := p
					if display == "" {
						display = "(empty)"
					}
					if isSelected {
						content.WriteString(lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(
							fmt.Sprintf("%s%d. %s", prefix, i+1, display)))
					} else {
						content.WriteString(dimStyle.Render(
							fmt.Sprintf("%s%d. %s", prefix, i+1, display)))
					}
					content.WriteString("\n")
				}
			}
			content.WriteString(dimStyle.Render("    [a: add, d: remove, enter: edit, up/down: navigate]"))
			content.WriteString("\n")
			// Show path suggestions dropdown when editing a multi-repo path.
			// Extract the suggestion rendering from the single-path View code into
			// a helper (or inline-duplicate the suggestion dropdown loop here) so
			// that autocomplete suggestions appear below the edited path entry.
		} else {
			for i, p := range d.multiRepoPaths {
				display := p
				if display == "" {
					display = "(empty)"
				}
				content.WriteString(dimStyle.Render(fmt.Sprintf("    %d. %s", i+1, display)))
				content.WriteString("\n")
			}
		}
	}
```

Also: when `d.multiRepoEnabled`, suppress the single path input rendering (wrap existing path rendering in `if !d.multiRepoEnabled`).

- [ ] **Step 16: Build and verify**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 17: Commit**

```bash
git add internal/ui/newdialog.go
git commit -m "feat: add multi-repo toggle and path list UI to new session dialog"
```

---

### Task 5: Wire Multi-Repo Into Session Creation in `home.go`

**Files:**
- Modify: `internal/ui/home.go:4131-4149` (extract multi-repo values, pass to create function)
- Modify: `internal/ui/home.go:5859-5941` (add multi-repo params, create temp dir with symlinks/worktrees)

- [ ] **Step 1: Guard Enter handler against multi-repo editing**

In `home.go`, in the Enter key handler for the new dialog (around line 4100-4128 where validation happens), add a guard BEFORE the validation/submit logic:

```go
		// Don't submit form while user is editing a multi-repo path entry
		if h.newDialog.IsMultiRepoEditing() {
			return h, nil
		}
```

This prevents the form from being submitted when the user presses Enter to confirm a path edit.

- [ ] **Step 2: Extract multi-repo values in session creation handler**

In `home.go`, after line 4134 (`epicID := ...`), add:

```go
		multiRepoPaths, multiRepoEnabled := h.newDialog.GetMultiRepoPaths()
```

Add these as new parameters to the `createSessionInGroupWithWorktreeAndOptions` call (lines 4136-4149). Use `multiRepoPaths[0]` as `path` when multi-repo is enabled:

```go
		return h, h.createSessionInGroupWithWorktreeAndOptions(
			name,
			path,
			command,
			groupPath,
			worktreePath,
			worktreeRepoRoot,
			branchName,
			geminiYoloMode,
			sandboxMode,
			epicRunnerMode,
			epicID,
			multiRepoEnabled,
			multiRepoPaths,
			toolOptionsJSON,
		)
```

- [ ] **Step 3: Update createSessionInGroupWithWorktreeAndOptions signature**

Add two new parameters after `epicID string`:

```go
func (h *Home) createSessionInGroupWithWorktreeAndOptions(
	name, path, command, groupPath, worktreePath, worktreeRepoRoot, worktreeBranch string,
	geminiYoloMode bool,
	sandboxEnabled bool,
	epicRunnerEnabled bool,
	epicID string,
	multiRepoEnabled bool,
	multiRepoPaths []string,
	toolOptionsJSON json.RawMessage,
) tea.Cmd {
```

- [ ] **Step 4: Add multi-repo temp directory setup**

**IMPORTANT:** When `multiRepoEnabled` is true, the existing single-repo worktree block (lines 5874-5884) should be SKIPPED. The multi-repo block handles worktree creation per-repo. Add a guard around the existing worktree block:

```go
		if worktreePath != "" && worktreeRepoRoot != "" && worktreeBranch != "" && !multiRepoEnabled {
```

After the epic runner config block (after line 5941), add:

```go
		// Apply multi-repo config.
		if multiRepoEnabled && len(multiRepoPaths) > 1 {
			inst.MultiRepoEnabled = true
			// First path is the primary (already set as ProjectPath via `path` arg)
			inst.AdditionalPaths = multiRepoPaths[1:]
			allPaths := inst.AllProjectPaths()

			if worktreeBranch != "" {
				// Multi-repo + worktree: create persistent parent dir with worktrees inside.
				home, _ := os.UserHomeDir()
				sanitizedBranch := strings.ReplaceAll(worktreeBranch, "/", "-")
				sanitizedBranch = strings.ReplaceAll(sanitizedBranch, " ", "-")
				parentDir := filepath.Join(home, ".agent-deck", "multi-repo-worktrees",
					fmt.Sprintf("%s-%s", sanitizedBranch, inst.ID[:8]))
				if mkErr := os.MkdirAll(parentDir, 0o755); mkErr != nil {
					return sessionCreatedMsg{err: fmt.Errorf("failed to create multi-repo worktree dir: %w", mkErr)}
				}
				if resolved, evalErr := filepath.EvalSymlinks(parentDir); evalErr == nil {
					parentDir = resolved
				}
				inst.MultiRepoTempDir = parentDir

				dirnames := session.DeduplicateDirnames(allPaths)
				var newProjectPath string
				var newAdditionalPaths []string
				for i, p := range allPaths {
					wtPath := filepath.Join(parentDir, dirnames[i])
					if git.IsGitRepo(p) {
						repoRoot, rootErr := git.GetWorktreeBaseRoot(p)
						if rootErr != nil {
							uiLog.Warn("multi_repo_worktree_skip", slog.String("path", p), slog.String("error", rootErr.Error()))
							_ = os.Symlink(p, wtPath)
							if i == 0 {
								newProjectPath = wtPath
							} else {
								newAdditionalPaths = append(newAdditionalPaths, wtPath)
							}
							continue
						}
						if err := git.CreateWorktree(repoRoot, wtPath, worktreeBranch); err != nil {
							uiLog.Warn("multi_repo_worktree_create_fail", slog.String("path", p), slog.String("error", err.Error()))
							_ = os.Symlink(p, wtPath)
							if i == 0 {
								newProjectPath = wtPath
							} else {
								newAdditionalPaths = append(newAdditionalPaths, wtPath)
							}
							continue
						}
						inst.MultiRepoWorktrees = append(inst.MultiRepoWorktrees, session.MultiRepoWorktree{
							OriginalPath: p,
							WorktreePath: wtPath,
							RepoRoot:     repoRoot,
							Branch:       worktreeBranch,
						})
						if i == 0 {
							newProjectPath = wtPath
						} else {
							newAdditionalPaths = append(newAdditionalPaths, wtPath)
						}
					} else {
						_ = os.Symlink(p, wtPath)
						if i == 0 {
							newProjectPath = wtPath
						} else {
							newAdditionalPaths = append(newAdditionalPaths, wtPath)
						}
					}
				}
				inst.ProjectPath = newProjectPath
				inst.AdditionalPaths = newAdditionalPaths
			} else {
				// Multi-repo without worktree: persistent parent dir with symlinks.
				home, _ := os.UserHomeDir()
				parentDir := filepath.Join(home, ".agent-deck", "multi-repo-worktrees", inst.ID[:8])
				if mkErr := os.MkdirAll(parentDir, 0o755); mkErr != nil {
					return sessionCreatedMsg{err: fmt.Errorf("failed to create multi-repo dir: %w", mkErr)}
				}
				if resolved, evalErr := filepath.EvalSymlinks(parentDir); evalErr == nil {
					parentDir = resolved
				}
				inst.MultiRepoTempDir = parentDir

				dirnames := session.DeduplicateDirnames(allPaths)
				var newProjectPath string
				var newAdditionalPaths []string
				for i, p := range allPaths {
					linkPath := filepath.Join(parentDir, dirnames[i])
					_ = os.Symlink(p, linkPath)
					if i == 0 {
						newProjectPath = linkPath
					} else {
						newAdditionalPaths = append(newAdditionalPaths, linkPath)
					}
				}
				inst.ProjectPath = newProjectPath
				inst.AdditionalPaths = newAdditionalPaths
			}
		}
```

- [ ] **Step 5: Build and verify**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 6: Commit**

```bash
git add internal/ui/home.go
git commit -m "feat: wire multi-repo into session creation with temp dir and symlinks"
```

---

### Task 6: Add Multi-Repo Cleanup on Session Delete and Fork Propagation

**Files:**
- Modify: `internal/ui/home.go:6200-6213` (deleteSession — add multi-repo cleanup)
- Modify: `internal/ui/home.go:6117-6178` (forkSessionCmdWithOptions — propagate multi-repo)

- [ ] **Step 1: Add multi-repo cleanup in deleteSession**

Update `deleteSession` (line 6200) to capture and clean up multi-repo state:

```go
func (h *Home) deleteSession(inst *session.Instance) tea.Cmd {
	id := inst.ID
	isWorktree := inst.IsWorktree()
	worktreePath := inst.WorktreePath
	worktreeRepoRoot := inst.WorktreeRepoRoot
	isMultiRepo := inst.IsMultiRepo()
	multiRepoTempDir := inst.MultiRepoTempDir
	multiRepoWorktrees := inst.MultiRepoWorktrees
	return func() tea.Msg {
		killErr := inst.Kill()
		if isWorktree {
			_ = git.RemoveWorktree(worktreeRepoRoot, worktreePath, false)
			_ = git.PruneWorktrees(worktreeRepoRoot)
		}
		if isMultiRepo {
			if multiRepoTempDir != "" {
				_ = os.RemoveAll(multiRepoTempDir)
			}
			for _, wt := range multiRepoWorktrees {
				_ = git.RemoveWorktree(wt.RepoRoot, wt.WorktreePath, false)
				_ = git.PruneWorktrees(wt.RepoRoot)
			}
		}
		return sessionDeletedMsg{deletedID: id, killErr: killErr}
	}
}
```

- [ ] **Step 2: Propagate multi-repo in forkSessionCmdWithOptions**

In `forkSessionCmdWithOptions` (line 6117), after the sandbox application (line 6165), add:

```go
		// Propagate multi-repo config from source.
		if source.IsMultiRepo() {
			inst.MultiRepoEnabled = true
			inst.AdditionalPaths = append([]string{}, source.AdditionalPaths...)
			if len(source.MultiRepoWorktrees) > 0 {
				inst.MultiRepoWorktrees = append([]session.MultiRepoWorktree{}, source.MultiRepoWorktrees...)
			}
			// Create new persistent dir with symlinks to shared worktrees/paths
			home, _ := os.UserHomeDir()
			parentDir := filepath.Join(home, ".agent-deck", "multi-repo-worktrees", inst.ID[:8])
			if mkErr := os.MkdirAll(parentDir, 0o755); mkErr != nil {
				return sessionForkedMsg{err: fmt.Errorf("failed to create multi-repo dir: %w", mkErr), sourceID: sourceID}
			}
			if resolved, evalErr := filepath.EvalSymlinks(parentDir); evalErr == nil {
				parentDir = resolved
			}
			inst.MultiRepoTempDir = parentDir
			allPaths := inst.AllProjectPaths()
			dirnames := session.DeduplicateDirnames(allPaths)
			var newProjectPath string
			var newAdditionalPaths []string
			for i, p := range allPaths {
				linkPath := filepath.Join(parentDir, dirnames[i])
				_ = os.Symlink(p, linkPath)
				if i == 0 {
					newProjectPath = linkPath
				} else {
					newAdditionalPaths = append(newAdditionalPaths, linkPath)
				}
			}
			inst.ProjectPath = newProjectPath
			inst.AdditionalPaths = newAdditionalPaths
		}
```

- [ ] **Step 3: Build and verify**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add internal/ui/home.go
git commit -m "feat: add multi-repo cleanup on delete and propagation on fork"
```

---

### Task 7: Integration Testing

- [ ] **Step 1: Build the full project**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build, no errors

- [ ] **Step 2: Run all existing tests**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go test ./... -count=1`
Expected: All tests pass

- [ ] **Step 3: Manual smoke test**

Run the TUI and verify:
1. Open new session dialog (N)
2. Press `m` on command field — multi-repo toggle activates
3. Path field transforms into stacked list
4. Press `a` to add paths, `enter` to edit, `d` to remove
5. Tab completion works in path entries
6. Validation requires at least 2 paths
7. Creating session creates temp dir with symlinks
8. Epic runner still works alongside multi-repo

- [ ] **Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: integration fixes for multi-repo support"
```
