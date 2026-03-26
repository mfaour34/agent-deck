# mdreview Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a keybinding (`p`) that opens a markdown file picker on any session, then launches mdreview as an ephemeral review session with auto-install support.

**Architecture:** A new `ReviewFileDialog` (multi-select file picker) is triggered from the home view. On confirm, an ephemeral session is created running `mdreview <files>`. A new `StatusCompleted` status marks review sessions that have exited. The hotkey system, help bar, and attach handler are extended to support the new flow.

**Tech Stack:** Go, Bubble Tea (TUI), tmux, filepath.WalkDir, exec.LookPath, pipx

**Spec:** `docs/superpowers/specs/2026-03-26-mdreview-integration-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/ui/hotkeys.go` | Modify | Register `hotkeyReview` constant, default binding, action order |
| `internal/session/instance.go` | Modify | Add `StatusCompleted`, `ReviewFiles` field, UpdateStatus mdreview check |
| `internal/ui/review_dialog.go` | Create | ReviewFileDialog: multi-select `.md` file picker |
| `internal/ui/review_dialog_test.go` | Create | Tests for file scanning and dialog logic |
| `internal/ui/home.go` | Modify | Wire `p` key, install check, create review session, help bar, re-attach |

---

### Task 1: Register Hotkey

**Files:**
- Modify: `internal/ui/hotkeys.go:8-96`

- [ ] **Step 1: Add hotkey constant**

In `internal/ui/hotkeys.go`, add to the const block (after `hotkeyReload` at line 35):

```go
	hotkeyReview           = "review"
```

- [ ] **Step 2: Add to action order**

In `hotkeyActionOrder` slice (after `hotkeyReload` entry, around line 65):

```go
	hotkeyReview,
```

- [ ] **Step 3: Add default binding**

In `defaultHotkeyBindings` map (after `hotkeyReload` entry, around line 95):

```go
	hotkeyReview:           "p",
```

- [ ] **Step 4: Build**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 5: Commit**

```bash
git add internal/ui/hotkeys.go
git commit -m "feat: register hotkeyReview with default binding 'p'"
```

---

### Task 2: Add StatusCompleted and ReviewFiles to Instance

**Files:**
- Modify: `internal/session/instance.go:38-47` (Status enum)
- Modify: `internal/session/instance.go:82-88` (Instance struct — add ReviewFiles)
- Modify: `internal/session/instance.go:2220-2238` (UpdateStatus switch)

- [ ] **Step 1: Add StatusCompleted to enum**

In `internal/session/instance.go`, after `StatusStarting` (line 47):

```go
	StatusCompleted Status = "completed" // Session finished (e.g., mdreview exited)
```

- [ ] **Step 2: Add ReviewFiles field to Instance struct**

After the `EpicID` field (line 80), add:

```go
	// Review session support
	ReviewFiles []string `json:"review_files,omitempty"` // Files selected for mdreview
```

- [ ] **Step 3: Add mdreview check in UpdateStatus**

In the `UpdateStatus` method's status mapping switch (line 2220-2238), change the `"inactive"` case:

```go
	case "inactive":
		// mdreview sessions that exit are "completed", not errors
		if i.Tool == "mdreview" {
			i.Status = StatusCompleted
		} else {
			i.Status = StatusError
		}
```

- [ ] **Step 3b: Add early return guard for StatusCompleted**

In `UpdateStatus`, BEFORE the `tmuxSession.Exists()` check (around line 2136), add a guard to prevent completed sessions from reverting to error on the next status poll:

```go
	// Completed sessions are terminal — no re-checking needed.
	if i.Status == StatusCompleted {
		return nil
	}
```

- [ ] **Step 4: Build**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 5: Commit**

```bash
git add internal/session/instance.go
git commit -m "feat: add StatusCompleted and ReviewFiles to Instance"
```

---

### Task 3: Create ReviewFileDialog

**Files:**
- Create: `internal/ui/review_dialog.go`
- Create: `internal/ui/review_dialog_test.go`

This is the largest task. The dialog follows the pattern of `skill_dialog.go`.

- [ ] **Step 1: Create the dialog file**

Create `internal/ui/review_dialog.go` with the full implementation:

```go
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ReviewFileDialog presents a multi-select list of .md files for mdreview.
type ReviewFileDialog struct {
	visible     bool
	width       int
	height      int
	projectPath string // root path to scan
	sessionName string // source session name (for title generation)
	sessionID   string // source session ID
	groupPath   string // source session group

	files       []string // relative paths to .md files
	selected    map[int]bool
	cursor      int
	scrollOff   int
	filterBuf   string
	filterUntil time.Time
	err         error
	confirmed   bool // set when user confirms selection with Enter
}

const reviewFilterTimeout = 1200 * time.Millisecond

// NewReviewFileDialog creates an uninitialized dialog.
func NewReviewFileDialog() *ReviewFileDialog {
	return &ReviewFileDialog{
		selected: make(map[int]bool),
	}
}

// Show populates the dialog with .md files from projectPath and makes it visible.
func (d *ReviewFileDialog) Show(projectPath, sessionName, sessionID, groupPath string, width, height int) {
	d.projectPath = projectPath
	d.sessionName = sessionName
	d.sessionID = sessionID
	d.groupPath = groupPath
	d.width = width
	d.height = height
	d.cursor = 0
	d.scrollOff = 0
	d.selected = make(map[int]bool)
	d.filterBuf = ""
	d.err = nil

	d.files = scanMarkdownFiles(projectPath)
	if len(d.files) == 0 {
		d.err = fmt.Errorf("no markdown files found in %s", projectPath)
	}
	d.visible = true
}

// Hide closes the dialog.
func (d *ReviewFileDialog) Hide() {
	d.visible = false
}

// IsVisible returns whether the dialog is showing.
func (d *ReviewFileDialog) IsVisible() bool {
	return d.visible
}

// IsConfirmed returns true after the user pressed Enter with selections.
// The parent should check this after Update, then call Hide and act on it.
func (d *ReviewFileDialog) IsConfirmed() bool {
	return d.confirmed
}

// SelectedFiles returns the absolute paths of selected files.
func (d *ReviewFileDialog) SelectedFiles() []string {
	var result []string
	for i, f := range d.files {
		if d.selected[i] {
			result = append(result, filepath.Join(d.projectPath, f))
		}
	}
	return result
}

// SelectedCount returns how many files are selected.
func (d *ReviewFileDialog) SelectedCount() int {
	return len(d.selected)
}

// SessionName returns the source session name.
func (d *ReviewFileDialog) SessionName() string {
	return d.sessionName
}

// SessionGroupPath returns the source session's group path.
func (d *ReviewFileDialog) SessionGroupPath() string {
	return d.groupPath
}

// ProjectPath returns the project path being scanned.
func (d *ReviewFileDialog) ProjectPath() string {
	return d.projectPath
}

// scanMarkdownFiles walks projectPath and returns relative paths of .md files.
// Skips .git, node_modules, vendor directories and symlinks.
func scanMarkdownFiles(root string) []string {
	var files []string
	skipDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"vendor":       true,
		".venv":        true,
		"__pycache__":  true,
	}

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors silently
		}
		// Skip symlinks
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		// Skip known noisy directories
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		// Collect .md files
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				files = append(files, rel)
			}
		}
		return nil
	})
	return files
}

// Update handles key messages for the dialog.
func (d *ReviewFileDialog) Update(msg tea.Msg) (*ReviewFileDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	// Clear expired filter
	if time.Now().After(d.filterUntil) {
		d.filterBuf = ""
	}

	maxVisible := d.maxVisible()

	switch keyMsg.String() {
	case "esc":
		d.Hide()
		return d, nil

	case "enter":
		if d.err != nil {
			d.Hide()
			return d, nil
		}
		if d.SelectedCount() == 0 {
			return d, nil // require at least 1 selection
		}
		// Signal confirmation — parent checks d.IsConfirmed()
		d.confirmed = true
		return d, nil

	case " ":
		if len(d.files) > 0 {
			if d.selected[d.cursor] {
				delete(d.selected, d.cursor)
			} else {
				d.selected[d.cursor] = true
			}
		}
		return d, nil

	case "a":
		// Toggle select all / none
		if len(d.selected) == len(d.files) {
			d.selected = make(map[int]bool)
		} else {
			for i := range d.files {
				d.selected[i] = true
			}
		}
		return d, nil

	case "j", "down":
		if d.cursor < len(d.files)-1 {
			d.cursor++
			if d.cursor >= d.scrollOff+maxVisible {
				d.scrollOff = d.cursor - maxVisible + 1
			}
		}
		return d, nil

	case "k", "up":
		if d.cursor > 0 {
			d.cursor--
			if d.cursor < d.scrollOff {
				d.scrollOff = d.cursor
			}
		}
		return d, nil

	case "G":
		d.cursor = len(d.files) - 1
		if d.cursor >= d.scrollOff+maxVisible {
			d.scrollOff = d.cursor - maxVisible + 1
		}
		return d, nil

	default:
		// Type-to-jump (like skill_dialog) — any single letter except 'a' (toggle-all)
		s := keyMsg.String()
		if len(s) == 1 && s != "a" && s != " " && ((s >= "a" && s <= "z") || (s >= "A" && s <= "Z")) {
			d.filterBuf += strings.ToLower(s)
			d.filterUntil = time.Now().Add(reviewFilterTimeout)
			d.jumpToFilter()
		}
	}

	return d, nil
}

func (d *ReviewFileDialog) jumpToFilter() {
	for i, f := range d.files {
		if strings.Contains(strings.ToLower(f), d.filterBuf) {
			d.cursor = i
			maxVisible := d.maxVisible()
			if d.cursor < d.scrollOff {
				d.scrollOff = d.cursor
			} else if d.cursor >= d.scrollOff+maxVisible {
				d.scrollOff = d.cursor - maxVisible + 1
			}
			break
		}
	}
}

func (d *ReviewFileDialog) maxVisible() int {
	// Reserve lines for header, footer, border
	max := d.height - 8
	if max < 3 {
		max = 3
	}
	return max
}

// View renders the dialog.
func (d *ReviewFileDialog) View() string {
	if !d.visible {
		return ""
	}

	dialogWidth := d.width - 4
	if dialogWidth > 80 {
		dialogWidth = 80
	}
	if dialogWidth < 40 {
		dialogWidth = 40
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorCyan)
	b.WriteString(titleStyle.Render("Review Markdown Files"))
	b.WriteString("\n")

	// Error state
	if d.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(ColorRed)
		b.WriteString(errStyle.Render(d.err.Error()))
		b.WriteString("\n\n")
		dimStyle := lipgloss.NewStyle().Foreground(ColorComment)
		b.WriteString(dimStyle.Render("Press Enter or Esc to close"))
		return d.wrapInBox(b.String(), dialogWidth)
	}

	// Counter
	dimStyle := lipgloss.NewStyle().Foreground(ColorComment)
	b.WriteString(dimStyle.Render(fmt.Sprintf("%d selected of %d files", d.SelectedCount(), len(d.files))))
	b.WriteString("\n\n")

	// File list
	maxVisible := d.maxVisible()
	endIdx := d.scrollOff + maxVisible
	if endIdx > len(d.files) {
		endIdx = len(d.files)
	}

	selectedStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(ColorText)
	checkStyle := lipgloss.NewStyle().Foreground(ColorGreen)

	if d.scrollOff > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", d.scrollOff)))
		b.WriteString("\n")
	}

	for i := d.scrollOff; i < endIdx; i++ {
		isCursor := i == d.cursor
		isSelected := d.selected[i]

		check := "[ ]"
		if isSelected {
			check = checkStyle.Render("[✓]")
		}

		prefix := "  "
		style := normalStyle
		if isCursor {
			prefix = "▸ "
			style = selectedStyle
		}

		line := fmt.Sprintf("%s%s %s", prefix, check, d.files[i])
		if isCursor || isSelected {
			b.WriteString(style.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	if endIdx < len(d.files) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", len(d.files)-endIdx)))
		b.WriteString("\n")
	}

	// Help line
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("[space: select, a: all, enter: confirm, esc: cancel]"))

	return d.wrapInBox(b.String(), dialogWidth)
}

func (d *ReviewFileDialog) wrapInBox(content string, width int) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(1, 2).
		Width(width)
	return boxStyle.Render(content)
}
```

- [ ] **Step 2: Create test file**

Create `internal/ui/review_dialog_test.go`:

```go
package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanMarkdownFiles(t *testing.T) {
	// Create temp directory structure
	dir := t.TempDir()

	// Create .md files
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not md"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "docs", "plan.md"), []byte("# Plan"), 0o644)

	// Create dirs that should be skipped
	_ = os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "node_modules", "pkg.md"), []byte("skip"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".git", "config.md"), []byte("skip"), 0o644)

	files := scanMarkdownFiles(dir)

	// Should find README.md and docs/plan.md, not node_modules or .git
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	if !found["README.md"] {
		t.Error("missing README.md")
	}
	if !found[filepath.Join("docs", "plan.md")] {
		t.Errorf("missing docs/plan.md, got: %v", files)
	}
}

func TestReviewDialogSelection(t *testing.T) {
	d := NewReviewFileDialog()
	dir := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.md"), []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "c.md"), []byte("c"), 0o644)

	d.Show(dir, "test", "id1", "group1", 80, 40)

	if !d.IsVisible() {
		t.Fatal("dialog should be visible")
	}
	if len(d.files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(d.files))
	}
	if d.SelectedCount() != 0 {
		t.Fatal("should start with 0 selected")
	}

	// Select first file
	d.selected[0] = true
	if d.SelectedCount() != 1 {
		t.Fatal("should have 1 selected")
	}

	// Verify absolute paths returned
	selected := d.SelectedFiles()
	if len(selected) != 1 {
		t.Fatalf("expected 1 selected file, got %d", len(selected))
	}
	if !filepath.IsAbs(selected[0]) {
		t.Errorf("expected absolute path, got %s", selected[0])
	}
}

func TestReviewDialogNoFiles(t *testing.T) {
	d := NewReviewFileDialog()
	dir := t.TempDir() // empty dir

	d.Show(dir, "test", "id1", "group1", 80, 40)

	if d.err == nil {
		t.Fatal("should have error when no .md files found")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go test ./internal/ui/ -run TestScanMarkdown -v && go test ./internal/ui/ -run TestReviewDialog -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/ui/review_dialog.go internal/ui/review_dialog_test.go
git commit -m "feat: add ReviewFileDialog with multi-select markdown file picker"
```

---

### Task 4: Wire Keybinding and Install Check in Home

**Files:**
- Modify: `internal/ui/home.go` (key handler, install check, review session creation)

- [ ] **Step 1: Add ReviewFileDialog field to Home struct**

In `internal/ui/home.go`, find the Home struct (around line 80-180) and add alongside the other dialog fields:

```go
	reviewDialog         *ReviewFileDialog
	mdreviewInstalled    bool // cached LookPath result
	mdreviewCheckDone    bool // whether we've checked yet
```

- [ ] **Step 2: Initialize dialog in NewHome**

In the `NewHome` function, add after other dialog initializations:

```go
	reviewDialog: NewReviewFileDialog(),
```

- [ ] **Step 3: Add message types for install flow**

Near the other message types (around line 480-500), add:

```go
// mdreviewInstallMsg signals completion of mdreview installation.
type mdreviewInstallMsg struct {
	err error
}

// mdreviewCheckMsg signals completion of mdreview availability check.
type mdreviewCheckMsg struct {
	installed bool
}
```

- [ ] **Step 4: Add the `p` key handler**

In the main key handler section (around line 4600-4700, near `"s"` for skills), add:

```go
	case h.actionKey(hotkeyReview):
		if item.Type != session.ItemTypeSession || item.Session == nil {
			return h, nil
		}
		// Don't review a review session
		if item.Session.Tool == "mdreview" {
			return h, nil
		}
		// Don't review SSH sessions
		if item.Session.IsSSH() {
			return h, nil
		}

		// Check if mdreview is installed
		if !h.mdreviewCheckDone {
			_, err := exec.LookPath("mdreview")
			h.mdreviewInstalled = err == nil
			h.mdreviewCheckDone = true
		}

		if !h.mdreviewInstalled {
			// Check for pipx
			_, pipxErr := exec.LookPath("pipx")
			if pipxErr != nil {
				h.setError(fmt.Errorf("mdreview requires pipx. Install pipx first: brew install pipx"))
				return h, nil
			}
			// Show install prompt via error bar (simple approach)
			h.setError(fmt.Errorf("installing mdreview from github.com/mfaour34/mdreview..."))
			return h, func() tea.Msg {
				cmd := exec.Command("pipx", "install", "git+https://github.com/mfaour34/mdreview")
				err := cmd.Run()
				return mdreviewInstallMsg{err: err}
			}
		}

		// Resolve project path
		projectPath := item.Session.WorktreePath
		if projectPath == "" {
			projectPath = item.Session.ProjectPath
		}
		if projectPath == "" {
			h.setError(fmt.Errorf("session has no project path"))
			return h, nil
		}

		h.reviewDialog.Show(
			projectPath,
			item.Session.Title,
			item.Session.ID,
			item.Session.GroupPath,
			h.width, h.height,
		)
		return h, nil
```

- [ ] **Step 5: Add import for `os/exec`**

Add `"os/exec"` to the import block of `home.go` if not already present.

- [ ] **Step 6: Handle install result message**

In the main `Update` switch (where other messages are handled), add:

```go
	case mdreviewInstallMsg:
		if msg.err != nil {
			h.setError(fmt.Sprintf("Failed to install mdreview: %v", msg.err))
			return h, nil
		}
		h.mdreviewInstalled = true
		h.clearError()
		h.setError(fmt.Errorf("mdreview installed successfully — press p again to review"))
		return h, nil
```

- [ ] **Step 7: Route dialog updates**

In the `Update` method, near where other dialog updates are routed (search for `mcpDialog` or `skillDialog` update routing), add:

```go
	if h.reviewDialog.IsVisible() {
		h.reviewDialog, cmd = h.reviewDialog.Update(msg)
		// Check if dialog was confirmed
		if h.reviewDialog.IsConfirmed() {
			files := h.reviewDialog.SelectedFiles()
			sessionName := h.reviewDialog.SessionName()
			groupPath := h.reviewDialog.SessionGroupPath()
			projectPath := h.reviewDialog.ProjectPath()
			h.reviewDialog.Hide()
			return h, h.createReviewSession(sessionName, groupPath, projectPath, files)
		}
		if !h.reviewDialog.IsVisible() {
			return h, nil // dialog was cancelled (esc)
		}
		return h, cmd
	}
```

- [ ] **Step 8: Add createReviewSession method**

```go
// createReviewSession creates an ephemeral mdreview session.
func (h *Home) createReviewSession(sourceSessionName, groupPath, projectPath string, files []string) tea.Cmd {
	return func() tea.Msg {
		if err := tmux.IsTmuxAvailable(); err != nil {
			return sessionCreatedMsg{err: fmt.Errorf("cannot create review session: %w", err)}
		}

		name := "review: " + sourceSessionName
		var inst *session.Instance
		if groupPath != "" {
			inst = session.NewInstanceWithGroupAndTool(name, projectPath, groupPath, "mdreview")
		} else {
			inst = session.NewInstanceWithTool(name, projectPath, "mdreview")
		}

		// Build command with shell-safe single-quoted file paths
		args := []string{"mdreview"}
		for _, f := range files {
			// Single-quote each path, escaping any internal single quotes
			escaped := strings.ReplaceAll(f, "'", "'\"'\"'")
			args = append(args, "'"+escaped+"'")
		}
		inst.Command = strings.Join(args, " ")
		inst.ReviewFiles = files

		if err := inst.Start(); err != nil {
			return sessionCreatedMsg{err: fmt.Errorf("failed to start review session: %w", err)}
		}

		return sessionCreatedMsg{instance: inst}
	}
}
```

- [ ] **Step 9: Add dialog View rendering**

In the `View()` method of Home, near where other dialog overlays are rendered (search for `mcpDialog.View()` or `skillDialog.View()`), add:

```go
	if h.reviewDialog.IsVisible() {
		return h.reviewDialog.View()
	}
```

- [ ] **Step 10: Build**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 11: Commit**

```bash
git add internal/ui/home.go
git commit -m "feat: wire review keybinding, install check, and review session creation"
```

---

### Task 5: Handle Re-attach on Completed Sessions

**Files:**
- Modify: `internal/ui/home.go:4415-4434` (Enter key handler)

- [ ] **Step 1: Add re-attach logic for completed sessions**

In the Enter key handler (line 4415-4434), before the existing `if item.Session.Exists()` check, add a completed session handler:

```go
			if item.Type == session.ItemTypeSession && item.Session != nil {
				// Completed review sessions: restart on Enter
				if item.Session.GetStatusThreadSafe() == session.StatusCompleted {
					if !h.hasActiveAnimation(item.Session.ID) {
						h.resumingSessions[item.Session.ID] = time.Now()
						return h, h.restartSession(item.Session)
					}
					return h, nil
				}
				if item.Session.Exists() {
```

This goes right after the `if item.Type == session.ItemTypeSession && item.Session != nil {` line (4418), before the `if item.Session.Exists()` check (4419).

- [ ] **Step 2: Build**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add internal/ui/home.go
git commit -m "feat: restart completed review sessions on Enter"
```

---

### Task 6: Add StatusCompleted Rendering

**Files:**
- Modify: `internal/ui/home.go` (session list rendering — wherever status icons/colors are mapped)

- [ ] **Step 1: Find the status-to-icon/color mapping**

Search `home.go` for where `StatusError`, `StatusIdle`, `StatusRunning` are mapped to icons or colors in the session list rendering. This is typically in a `renderSession` or `renderItem` function, or inline in `View()`.

- [ ] **Step 2: Add StatusCompleted case**

Add `StatusCompleted` to the status rendering with:
- Icon: `"✓"` (checkmark)
- Color: `ColorComment` (dimmed, consistent with inactive styling)
- The session title should also render with `ColorComment` foreground

Example (adapt to match the existing pattern):
```go
case session.StatusCompleted:
	statusIcon = "✓"
	statusColor = ColorComment
```

- [ ] **Step 3: Build**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add internal/ui/home.go
git commit -m "feat: render StatusCompleted with checkmark icon and dimmed style"
```

---

### Task 7: Add Help Bar Entries

**Files:**
- Modify: `internal/ui/home.go` (renderHelpBarFull, renderHelpBarCompact, renderHelpBarMinimal)

- [ ] **Step 1: Add to renderHelpBarFull**

In `renderHelpBarFull()` (around line 8003-8060), in the session section, after the `notesKey` block and before the secondary hints, add:

```go
			if reviewKey := h.actionKey(hotkeyReview); reviewKey != "" {
				if item.Session != nil && item.Session.Tool != "mdreview" && !item.Session.IsSSH() && item.Session.ProjectPath != "" {
					primaryHints = append(primaryHints, h.helpKey(reviewKey, "Review"))
				}
			}
```

- [ ] **Step 2: Add to renderHelpBarCompact**

In `renderHelpBarCompact()` (around line 7836-7871), in the session section, after the `hotkeyEditNotes` block:

```go
			if key := h.actionKey(hotkeyReview); key != "" {
				if item.Session != nil && item.Session.Tool != "mdreview" && !item.Session.IsSSH() && item.Session.ProjectPath != "" {
					contextHints = append(contextHints, h.helpKeyShort(key, "Rev"))
				}
			}
```

- [ ] **Step 3: Add to renderHelpBarMinimal**

In `renderHelpBarMinimal()` (around line 7745-7769), in the session section, after the `notesRendered` block:

```go
			if item.Session != nil && item.Session.Tool != "mdreview" && !item.Session.IsSSH() && item.Session.ProjectPath != "" {
				reviewRendered := renderKeys(h.actionKey(hotkeyReview))
				if reviewRendered != "" {
					contextKeys += " " + reviewRendered
				}
			}
```

- [ ] **Step 4: Build**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 5: Commit**

```bash
git add internal/ui/home.go
git commit -m "feat: add Review to help bar in all responsive tiers"
```

---

### Task 8: Integration Testing

- [ ] **Step 1: Build the full project**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go build ./...`
Expected: Clean build

- [ ] **Step 2: Run all tests**

Run: `cd /Users/mohammed.faour/Desktop/misc/agent-deck && go test ./... -count=1`
Expected: All tests pass

- [ ] **Step 3: Manual smoke test**

Run the TUI and verify:
1. Press `p` on a session — if mdreview not installed, see install prompt
2. After install, press `p` — file picker shows `.md` files from project
3. Navigate with j/k, select with space, toggle all with `a`
4. Press Enter — review session appears in same group
5. Attach to review session — mdreview launches with selected files
6. Quit mdreview — session shows as completed (not error)
7. Press Enter on completed session — mdreview relaunches
8. Help bar shows `[p]Review` when on a non-review session
9. Help bar does NOT show `[p]Review` on review sessions or SSH sessions

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: integration fixes for mdreview review sessions"
```
