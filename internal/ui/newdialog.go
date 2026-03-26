package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asheshgoplani/agent-deck/internal/git"
	"github.com/asheshgoplani/agent-deck/internal/session"
	"github.com/asheshgoplani/agent-deck/internal/statedb"
)

// focusTarget identifies a focusable element in the new session dialog.
type focusTarget int

const (
	focusName        focusTarget = iota
	focusPath                    // project path input(s).
	focusCommand                 // tool/command picker.
	focusWorktree                // worktree checkbox.
	focusSandbox                 // sandbox checkbox.
	focusEpicRunner              // epic runner checkbox.
	focusEpicID                  // epic ID input (conditional — only when epic runner enabled).
	focusInherited               // inherited Docker settings toggle (conditional).
	focusBranch                  // branch input (conditional — only when worktree enabled).
	focusOptions                 // tool-specific options panel (conditional).
)

// settingDisplay pairs a label with a formatted value for read-only display.
type settingDisplay struct {
	label string
	value string
}

// NewDialog represents the new session creation dialog.
type NewDialog struct {
	nameInput            textinput.Model
	pathInputs           []textinput.Model  // Dynamic list of path inputs.
	activePathIdx        int                // Which path field is currently active.
	commandInput         textinput.Model
	claudeOptions        *ClaudeOptionsPanel // Claude-specific options (concrete for value extraction).
	geminiOptions        *YoloOptionsPanel   // Gemini YOLO panel (concrete for value extraction).
	codexOptions         *YoloOptionsPanel   // Codex YOLO panel (concrete for value extraction).
	toolOptions          OptionsPanel        // Currently active tool options panel (nil if none).
	focusTargets         []focusTarget       // Ordered list of active focusable elements.
	focusIndex           int                 // Index into focusTargets.
	width                int
	height               int
	visible              bool
	presetCommands       []string
	commandCursor        int
	parentGroupPath      string
	parentGroupName      string
	pathSuggestions      []string // filtered subset of path suggestions shown in dropdown.
	allPathSuggestions   []string // full unfiltered set of path suggestions.
	pathSuggestionCursor int      // tracks selected suggestion in dropdown.
	suggestionNavigated  bool     // tracks if user explicitly navigated suggestions.
	pathSoftSelected     bool     // true when path text is "soft selected" (ready to replace on type).
	// Worktree support.
	worktreeEnabled bool
	branchInput     textinput.Model
	branchAutoSet   bool   // true if branch was auto-derived from session name.
	branchPrefix    string // configured prefix for auto-generated branch names.
	// Docker sandbox support.
	sandboxEnabled    bool
	inheritedExpanded bool             // whether the inherited settings section is expanded.
	inheritedSettings []settingDisplay // non-default Docker config values to display.
	// Epic runner support.
	epicRunnerEnabled bool
	epicIDInput       textinput.Model
	// Inline validation error displayed inside the dialog.
	validationErr string
	pathCycler       session.CompletionCycler // Path autocomplete state.
	// Recent sessions picker.
	recentSessions      []*statedb.RecentSessionRow
	recentSessionCursor int
	showRecentPicker    bool
	recentSnapshot      *dialogSnapshot // saved state to restore on Esc
}

// dialogSnapshot captures form state so the recent picker can restore on cancel.
type dialogSnapshot struct {
	name            string
	paths           []string // values from all pathInputs
	commandCursor   int
	commandInput    string
	sandboxEnabled    bool
	worktreeEnabled   bool
	epicRunnerEnabled bool
	branch            string
	branchAutoSet     bool
	claudeOptions     *session.ClaudeOptions
	geminiYolo        bool
	codexYolo         bool
}

// buildPresetCommands returns the list of commands for the picker,
// including any custom tools from config.toml.
func buildPresetCommands() []string {
	presets := []string{"", "claude", "gemini", "opencode", "codex", "pi"}
	if customTools := session.GetCustomToolNames(); len(customTools) > 0 {
		presets = append(presets, customTools...)
	}
	return presets
}

// buildInheritedSettings returns display pairs for non-default Docker config values.
func buildInheritedSettings(docker session.DockerSettings) []settingDisplay {
	var settings []settingDisplay
	if docker.DefaultImage != "" {
		settings = append(settings, settingDisplay{label: "Image", value: docker.DefaultImage})
	}
	if docker.CPULimit != "" {
		settings = append(settings, settingDisplay{label: "CPU Limit", value: docker.CPULimit})
	}
	if docker.MemoryLimit != "" {
		settings = append(settings, settingDisplay{label: "Memory Limit", value: docker.MemoryLimit})
	}
	if docker.MountSSH {
		settings = append(settings, settingDisplay{label: "Mount SSH", value: "yes"})
	}
	if len(docker.VolumeIgnores) > 0 {
		settings = append(
			settings,
			settingDisplay{label: "Volume Ignores", value: fmt.Sprintf("%d items", len(docker.VolumeIgnores))},
		)
	}
	if len(docker.Environment) > 0 {
		settings = append(
			settings,
			settingDisplay{label: "Env Vars", value: fmt.Sprintf("%d items", len(docker.Environment))},
		)
	}
	return settings
}

// NewNewDialog creates a new NewDialog instance
func NewNewDialog() *NewDialog {
	// Create name input
	nameInput := textinput.New()
	nameInput.Placeholder = "session-name"
	nameInput.Focus()
	nameInput.CharLimit = MaxNameLength
	nameInput.Width = 40

	// Create first path input pre-filled with cwd
	pathInput := textinput.New()
	pathInput.Placeholder = "~/project/path"
	pathInput.CharLimit = 256
	pathInput.Width = 40
	pathInput.ShowSuggestions = false // we use our own dropdown with filtering

	// Get current working directory for default path
	cwd, err := os.Getwd()
	if err == nil {
		pathInput.SetValue(cwd)
	}
	pathInputs := []textinput.Model{pathInput}

	// Create command input
	commandInput := textinput.New()
	commandInput.Placeholder = "custom command"
	commandInput.CharLimit = 100
	commandInput.Width = 40

	// Create branch input for worktree
	branchInput := textinput.New()
	branchInput.Placeholder = "feature/branch-name"
	branchInput.CharLimit = 100
	branchInput.Width = 40

	// Create epic ID input for epic runner
	epicIDInput := textinput.New()
	epicIDInput.Placeholder = "MOVE-123"
	epicIDInput.CharLimit = 20
	epicIDInput.Width = 40

	dlg := &NewDialog{
		nameInput:       nameInput,
		pathInputs:      pathInputs,
		activePathIdx:   0,
		commandInput:    commandInput,
		branchInput:     branchInput,
		epicIDInput:     epicIDInput,
		claudeOptions:   NewClaudeOptionsPanel(),
		geminiOptions:   NewYoloOptionsPanel("Gemini", "YOLO mode - auto-approve all"),
		codexOptions:    NewYoloOptionsPanel("Codex", "YOLO mode - bypass approvals and sandbox"),
		focusIndex:      0,
		visible:         false,
		presetCommands:  buildPresetCommands(),
		commandCursor:   0,
		parentGroupPath: "default",
		parentGroupName: "default",
		worktreeEnabled: false,
		branchPrefix:    "feature/",
	}
	dlg.updateToolOptions() // Also calls rebuildFocusTargets.
	return dlg
}

// activePathInput returns a pointer to the currently active path input.
func (d *NewDialog) activePathInput() *textinput.Model {
	if d.activePathIdx < 0 || d.activePathIdx >= len(d.pathInputs) {
		d.activePathIdx = 0
	}
	return &d.pathInputs[d.activePathIdx]
}

// ensureEmptyTrailingPath adds a new empty path field if the last one is non-empty.
func (d *NewDialog) ensureEmptyTrailingPath() {
	if len(d.pathInputs) == 0 || strings.TrimSpace(d.pathInputs[len(d.pathInputs)-1].Value()) != "" {
		ti := textinput.New()
		ti.Placeholder = "additional path..."
		ti.CharLimit = 256
		ti.Width = d.pathInputs[0].Width
		ti.ShowSuggestions = false
		d.pathInputs = append(d.pathInputs, ti)
	}
}

// trimEmptyTrailingPaths removes trailing empty path fields, keeping at least 1.
func (d *NewDialog) trimEmptyTrailingPaths() {
	for len(d.pathInputs) > 1 && strings.TrimSpace(d.pathInputs[len(d.pathInputs)-1].Value()) == "" {
		d.pathInputs = d.pathInputs[:len(d.pathInputs)-1]
	}
	if d.activePathIdx >= len(d.pathInputs) {
		d.activePathIdx = len(d.pathInputs) - 1
	}
}

// ShowInGroup shows the dialog with a pre-selected parent group and optional default path
func (d *NewDialog) ShowInGroup(groupPath, groupName, defaultPath string) {
	if groupPath == "" {
		groupPath = "default"
		groupName = "default"
	}
	d.parentGroupPath = groupPath
	d.parentGroupName = groupName
	d.visible = true
	d.focusIndex = 0
	d.validationErr = ""
	d.nameInput.SetValue("")
	d.nameInput.Focus()
	d.suggestionNavigated = false // reset on show
	d.pathSuggestionCursor = 0    // reset cursor too
	d.pathCycler.Reset()          // clear stale autocomplete matches from previous show
	d.showRecentPicker = false    // reset recent picker
	d.recentSessionCursor = 0
	d.claudeOptions.Blur()
	d.geminiOptions.Blur()
	d.codexOptions.Blur()
	// Keep commandCursor at previously set default (don't reset to 0)
	d.updateToolOptions()
	// Reset worktree fields.
	d.worktreeEnabled = false
	d.branchInput.SetValue("")
	d.branchAutoSet = false
	d.branchPrefix = "feature/" // default; overridden below if config provides one.
	// Reset sandbox from global config default.
	d.sandboxEnabled = false
	d.inheritedExpanded = false
	d.inheritedSettings = nil
	// Reset epic runner.
	d.epicRunnerEnabled = false
	d.epicIDInput.SetValue("")
	// Reset path inputs to a single field.
	d.activePathIdx = 0
	pathVal := ""
	if defaultPath != "" {
		pathVal = defaultPath
	} else {
		cwd, err := os.Getwd()
		if err == nil {
			pathVal = cwd
		}
	}
	firstPath := textinput.New()
	firstPath.Placeholder = "~/project/path"
	firstPath.CharLimit = 256
	firstPath.Width = 40
	firstPath.ShowSuggestions = false
	firstPath.SetValue(pathVal)
	d.pathInputs = []textinput.Model{firstPath}
	d.pathSoftSelected = true // activate soft-select for pre-filled path.
	// Initialize tool options from global config.
	d.geminiOptions.SetDefaults(false)
	d.codexOptions.SetDefaults(false)
	if userConfig, err := session.LoadUserConfig(); err == nil && userConfig != nil {
		d.geminiOptions.SetDefaults(userConfig.Gemini.YoloMode)
		d.codexOptions.SetDefaults(userConfig.Codex.YoloMode)
		d.claudeOptions.SetDefaults(userConfig)
		d.sandboxEnabled = userConfig.Docker.DefaultEnabled
		d.inheritedSettings = buildInheritedSettings(userConfig.Docker)
		d.branchPrefix = userConfig.Worktree.Prefix()
	}
	d.branchInput.Placeholder = d.branchPrefix + "branch-name"
	d.rebuildFocusTargets()
}

// SetDefaultTool sets the pre-selected command based on tool name
// Call this before Show/ShowInGroup to apply user's preferred default
func (d *NewDialog) SetDefaultTool(tool string) {
	if tool == "" {
		d.commandCursor = 0 // Default to shell
		return
	}

	// Find the tool in preset commands
	for i, cmd := range d.presetCommands {
		if cmd == tool {
			d.commandCursor = i
			d.updateToolOptions()
			return
		}
	}

	// Tool not found in presets, default to shell
	d.commandCursor = 0
	d.updateToolOptions()
}

// GetSelectedGroup returns the parent group path
func (d *NewDialog) GetSelectedGroup() string {
	return d.parentGroupPath
}

// SetSize sets the dialog dimensions
func (d *NewDialog) SetSize(width, height int) {
	d.width = width
	d.height = height
}

// SetPathSuggestions sets the available path suggestions for autocomplete
func (d *NewDialog) SetPathSuggestions(paths []string) {
	d.allPathSuggestions = paths
	d.pathSuggestions = paths
	d.pathSuggestionCursor = 0
}

// IsRecentPickerOpen returns whether the recent sessions picker is visible.
func (d *NewDialog) IsRecentPickerOpen() bool {
	return d.showRecentPicker && len(d.recentSessions) > 0
}

// SetRecentSessions sets the list of recently deleted session configs.
func (d *NewDialog) SetRecentSessions(sessions []*statedb.RecentSessionRow) {
	d.recentSessions = sessions
	d.recentSessionCursor = 0
	d.showRecentPicker = false
}

// saveSnapshot captures current form state so the picker can restore on cancel.
func (d *NewDialog) saveSnapshot() *dialogSnapshot {
	claudeOpts := d.claudeOptions.GetOptions()
	if claudeOpts != nil {
		copy := *claudeOpts
		claudeOpts = &copy
	}

	var paths []string
	for _, pi := range d.pathInputs {
		paths = append(paths, pi.Value())
	}

	return &dialogSnapshot{
		name:              d.nameInput.Value(),
		paths:             paths,
		commandCursor:     d.commandCursor,
		commandInput:      d.commandInput.Value(),
		sandboxEnabled:    d.sandboxEnabled,
		worktreeEnabled:   d.worktreeEnabled,
		epicRunnerEnabled: d.epicRunnerEnabled,
		branch:            d.branchInput.Value(),
		branchAutoSet:     d.branchAutoSet,
		claudeOptions:     claudeOpts,
		geminiYolo:        d.geminiOptions.GetYoloMode(),
		codexYolo:         d.codexOptions.GetYoloMode(),
	}
}

// restoreSnapshot restores form state from a snapshot.
func (d *NewDialog) restoreSnapshot(s *dialogSnapshot) {
	d.nameInput.SetValue(s.name)
	// Rebuild pathInputs from snapshot.
	d.pathInputs = nil
	for _, p := range s.paths {
		ti := textinput.New()
		ti.Placeholder = "~/project/path"
		ti.CharLimit = 256
		ti.Width = 40
		ti.ShowSuggestions = false
		ti.SetValue(p)
		d.pathInputs = append(d.pathInputs, ti)
	}
	if len(d.pathInputs) == 0 {
		ti := textinput.New()
		ti.Placeholder = "~/project/path"
		ti.CharLimit = 256
		ti.Width = 40
		ti.ShowSuggestions = false
		d.pathInputs = []textinput.Model{ti}
	}
	d.activePathIdx = 0
	d.commandCursor = s.commandCursor
	d.commandInput.SetValue(s.commandInput)
	d.sandboxEnabled = s.sandboxEnabled
	d.worktreeEnabled = s.worktreeEnabled
	d.epicRunnerEnabled = s.epicRunnerEnabled
	d.branchInput.SetValue(s.branch)
	d.branchAutoSet = s.branchAutoSet
	if s.claudeOptions != nil {
		d.claudeOptions.SetFromOptions(s.claudeOptions)
	}
	d.geminiOptions.SetDefaults(s.geminiYolo)
	d.codexOptions.SetDefaults(s.codexYolo)
	d.updateToolOptions()
	d.rebuildFocusTargets()
}

// previewRecentSession pre-fills the dialog from a recent session row (keeps picker open).
func (d *NewDialog) previewRecentSession(rs *statedb.RecentSessionRow) {
	d.nameInput.SetValue(rs.Title)
	if len(d.pathInputs) > 0 {
		d.pathInputs[0].SetValue(rs.ProjectPath)
	}

	// Default to shell/custom command mode.
	d.commandCursor = 0
	d.commandInput.SetValue("")

	// Set command/tool.
	if rs.Tool == "" || rs.Tool == "shell" {
		d.commandInput.SetValue(strings.TrimSpace(rs.Command))
	} else {
		matched := false
		for i, cmd := range d.presetCommands {
			if cmd == rs.Tool {
				d.commandCursor = i
				matched = true
				break
			}
		}
		// If the saved tool no longer exists, fall back to shell/custom command.
		if !matched {
			d.commandCursor = 0
			d.commandInput.SetValue(strings.TrimSpace(rs.Command))
		}
	}
	d.updateToolOptions()

	// Apply tool-specific options
	if len(rs.ToolOptions) > 0 && string(rs.ToolOptions) != "{}" {
		switch {
		case session.IsClaudeCompatible(rs.Tool):
			var wrapper session.ToolOptionsWrapper
			if err := json.Unmarshal(rs.ToolOptions, &wrapper); err == nil && wrapper.Tool == "claude" {
				var opts session.ClaudeOptions
				if err := json.Unmarshal(wrapper.Options, &opts); err == nil {
					d.claudeOptions.SetFromOptions(&opts)
				}
			}
		case rs.Tool == "gemini":
			if rs.GeminiYoloMode != nil {
				d.geminiOptions.SetDefaults(*rs.GeminiYoloMode)
			}
		case rs.Tool == "codex":
			var wrapper session.ToolOptionsWrapper
			if err := json.Unmarshal(rs.ToolOptions, &wrapper); err == nil && wrapper.Tool == "codex" {
				var opts session.CodexOptions
				if err := json.Unmarshal(wrapper.Options, &opts); err == nil && opts.YoloMode != nil {
					d.codexOptions.SetDefaults(*opts.YoloMode)
				}
			}
		}
	}

	d.sandboxEnabled = rs.SandboxEnabled

	// Reset worktree (ephemeral, never pre-filled)
	d.worktreeEnabled = false
	d.branchInput.SetValue("")
	d.branchAutoSet = false

	// Reset to single path.
	d.pathInputs = d.pathInputs[:1]
	d.activePathIdx = 0

	d.rebuildFocusTargets()
}

// filterPathSuggestions filters allPathSuggestions by the current path input value
func (d *NewDialog) filterPathSuggestions() {
	query := strings.ToLower(strings.TrimSpace(d.activePathInput().Value()))
	if query == "" {
		d.pathSuggestions = d.allPathSuggestions
	} else {
		filtered := make([]string, 0)
		for _, p := range d.allPathSuggestions {
			if strings.Contains(strings.ToLower(p), query) {
				filtered = append(filtered, p)
			}
		}
		d.pathSuggestions = filtered
	}
	if d.pathSuggestionCursor >= len(d.pathSuggestions) {
		d.pathSuggestionCursor = 0
	}
}

// Show makes the dialog visible (uses default group)
func (d *NewDialog) Show() {
	d.ShowInGroup("default", "default", "")
}

// Hide hides the dialog
func (d *NewDialog) Hide() {
	d.visible = false
}

// IsVisible returns whether the dialog is visible
func (d *NewDialog) IsVisible() bool {
	return d.visible
}

// GetValues returns the current dialog values with expanded paths
func (d *NewDialog) GetValues() (name, path, command string) {
	name = strings.TrimSpace(d.nameInput.Value())
	// Fix: sanitize input to remove surrounding quotes that cause path issues
	rawPath := ""
	if len(d.pathInputs) > 0 {
		rawPath = d.pathInputs[0].Value()
	}
	path = strings.Trim(strings.TrimSpace(rawPath), "'\"")

	// Fix malformed paths that have ~ in the middle (e.g., "/some/path~/actual/path")
	// This can happen when textinput suggestion appends instead of replaces
	if idx := strings.Index(path, "~/"); idx > 0 {
		path = path[idx:]
	}

	// Expand environment variables and ~ prefix
	path = session.ExpandPath(path)

	// Get command - either from preset or custom input
	if d.commandCursor < len(d.presetCommands) {
		command = d.presetCommands[d.commandCursor]
	}
	if command == "" && d.commandInput.Value() != "" {
		command = strings.TrimSpace(d.commandInput.Value())
	}

	return name, path, command
}

// ToggleWorktree toggles the worktree checkbox.
// When enabling, auto-populates the branch name from the session name.
func (d *NewDialog) ToggleWorktree() {
	d.worktreeEnabled = !d.worktreeEnabled
	if d.worktreeEnabled {
		d.autoBranchFromName()
	}
	d.rebuildFocusTargets()
}

// autoBranchFromName sets the branch input to "<prefix><session-name>" if the
// name field is non-empty and the branch hasn't been manually edited.
func (d *NewDialog) autoBranchFromName() {
	name := strings.TrimSpace(d.nameInput.Value())
	if name == "" {
		return
	}
	branch := d.branchPrefix + name
	d.branchInput.SetValue(branch)
	d.branchAutoSet = true
}

// IsWorktreeEnabled returns whether worktree mode is enabled
func (d *NewDialog) IsWorktreeEnabled() bool {
	return d.worktreeEnabled
}

// GetValuesWithWorktree returns all values including worktree settings
func (d *NewDialog) GetValuesWithWorktree() (name, path, command, branch string, worktreeEnabled bool) {
	name, path, command = d.GetValues()
	branch = strings.TrimSpace(d.branchInput.Value())
	worktreeEnabled = d.worktreeEnabled
	return
}

// IsGeminiYoloMode returns whether YOLO mode is enabled for Gemini
func (d *NewDialog) IsGeminiYoloMode() bool {
	return d.geminiOptions.GetYoloMode()
}

// GetCodexYoloMode returns the Codex YOLO mode state
func (d *NewDialog) GetCodexYoloMode() bool {
	return d.codexOptions.GetYoloMode()
}

// IsSandboxEnabled returns whether Docker sandbox mode is enabled.
func (d *NewDialog) IsSandboxEnabled() bool {
	return d.sandboxEnabled
}

// ToggleSandbox toggles Docker sandbox mode.
func (d *NewDialog) ToggleSandbox() {
	d.sandboxEnabled = !d.sandboxEnabled
	d.rebuildFocusTargets()
}

// IsEpicRunnerEnabled returns whether epic runner mode is enabled.
func (d *NewDialog) IsEpicRunnerEnabled() bool {
	return d.epicRunnerEnabled
}

// ToggleEpicRunner toggles epic runner mode.
func (d *NewDialog) ToggleEpicRunner() {
	d.epicRunnerEnabled = !d.epicRunnerEnabled
	d.rebuildFocusTargets()
}

// GetEpicID returns the epic ID entered by the user.
func (d *NewDialog) GetEpicID() string {
	return strings.TrimSpace(d.epicIDInput.Value())
}

// GetMultiRepoPaths returns expanded, non-empty paths. Multi-repo is implicit:
// returns (paths, true) when there are 2+ non-empty paths.
func (d *NewDialog) GetMultiRepoPaths() ([]string, bool) {
	var paths []string
	for _, pi := range d.pathInputs {
		p := strings.TrimSpace(pi.Value())
		if p == "" {
			continue
		}
		p = strings.Trim(p, "'\"")
		p = session.ExpandPath(p)
		paths = append(paths, p)
	}
	if len(paths) > 1 {
		return paths, true
	}
	return nil, false
}

// IsMultiRepoEditing always returns false (multi-repo is now implicit via dynamic path fields).
func (d *NewDialog) IsMultiRepoEditing() bool {
	return false
}

// GetSelectedCommand returns the currently selected command/tool
func (d *NewDialog) GetSelectedCommand() string {
	if d.commandCursor >= 0 && d.commandCursor < len(d.presetCommands) {
		return d.presetCommands[d.commandCursor]
	}
	return ""
}

// GetClaudeOptions returns the Claude-specific options (only relevant if command is "claude")
func (d *NewDialog) GetClaudeOptions() *session.ClaudeOptions {
	if !d.isClaudeSelected() {
		return nil
	}
	return d.claudeOptions.GetOptions()
}

// isClaudeSelected returns true if the selected command is Claude or a claude-compatible custom tool
func (d *NewDialog) isClaudeSelected() bool {
	if d.commandCursor < 0 || d.commandCursor >= len(d.presetCommands) {
		return false
	}
	return session.IsClaudeCompatible(d.presetCommands[d.commandCursor])
}

// isTextInputFocused returns true when a text input is actively focused (typing goes to an input field).
func (d *NewDialog) isTextInputFocused() bool {
	switch d.currentTarget() {
	case focusName, focusPath, focusBranch:
		return true
	case focusCommand:
		return d.commandCursor == 0
	case focusEpicID:
		return true
	default:
		return false
	}
}

// Validate checks if the dialog values are valid and returns an error message if not
func (d *NewDialog) Validate() string {
	name := strings.TrimSpace(d.nameInput.Value())

	// Check for empty name
	if name == "" {
		return "Session name cannot be empty"
	}

	// Check name length
	if len(name) > MaxNameLength {
		return fmt.Sprintf("Session name too long (max %d characters)", MaxNameLength)
	}

	// Check for empty first path
	firstPath := ""
	if len(d.pathInputs) > 0 {
		firstPath = strings.Trim(strings.TrimSpace(d.pathInputs[0].Value()), "'\"")
	}
	if firstPath == "" {
		return "Project path cannot be empty"
	}

	// Check for duplicate paths (when multiple non-empty paths exist).
	seen := make(map[string]bool)
	for _, pi := range d.pathInputs {
		p := strings.TrimSpace(pi.Value())
		if p == "" {
			continue
		}
		expanded := session.ExpandPath(strings.Trim(p, "'\""))
		if seen[expanded] {
			return "Duplicate paths detected"
		}
		seen[expanded] = true
	}

	// Validate epic ID if epic runner is enabled
	if d.epicRunnerEnabled {
		epicID := strings.TrimSpace(d.epicIDInput.Value())
		if epicID == "" {
			return "Epic ID required for epic runner"
		}
	}

	// Validate worktree branch if enabled
	if d.worktreeEnabled {
		branch := strings.TrimSpace(d.branchInput.Value())
		if branch == "" {
			return "Branch name required for worktree"
		}
		if err := git.ValidateBranchName(branch); err != nil {
			return err.Error()
		}
	}

	return "" // Valid
}

// SetError sets an inline validation error displayed inside the dialog
func (d *NewDialog) SetError(msg string) {
	d.validationErr = msg
}

// ClearError clears the inline validation error
func (d *NewDialog) ClearError() {
	d.validationErr = ""
}

// currentTarget returns the focusTarget at the current focusIndex.
func (d *NewDialog) currentTarget() focusTarget {
	if d.focusIndex < 0 || d.focusIndex >= len(d.focusTargets) {
		return focusName
	}
	return d.focusTargets[d.focusIndex]
}

// indexOf returns the index of target in focusTargets, or -1 if absent.
func (d *NewDialog) indexOf(target focusTarget) int {
	for i, t := range d.focusTargets {
		if t == target {
			return i
		}
	}
	return -1
}

// rebuildFocusTargets builds the ordered list of active focusable elements
// based on current dialog state (sandbox, worktree, tool options visibility).
func (d *NewDialog) rebuildFocusTargets() {
	targets := []focusTarget{focusName, focusPath, focusCommand, focusWorktree, focusSandbox, focusEpicRunner}
	if d.epicRunnerEnabled {
		targets = append(targets, focusEpicID)
	}
	if d.sandboxEnabled && len(d.inheritedSettings) > 0 {
		targets = append(targets, focusInherited)
	}
	if d.worktreeEnabled {
		targets = append(targets, focusBranch)
	}
	if d.toolOptions != nil {
		targets = append(targets, focusOptions)
	}
	d.focusTargets = targets
	// Clamp focusIndex to valid range.
	if d.focusIndex >= len(d.focusTargets) {
		d.focusIndex = len(d.focusTargets) - 1
	}
	if d.focusIndex < 0 {
		d.focusIndex = 0
	}
}

// updateToolOptions sets d.toolOptions to the panel matching the current tool selection.
func (d *NewDialog) updateToolOptions() {
	cmd := d.GetSelectedCommand()
	switch {
	case session.IsClaudeCompatible(cmd):
		d.toolOptions = d.claudeOptions
	case cmd == "gemini":
		d.toolOptions = d.geminiOptions
	case cmd == "codex":
		d.toolOptions = d.codexOptions
	default:
		d.toolOptions = nil
	}
	d.rebuildFocusTargets()
}

func (d *NewDialog) updateFocus() {
	d.nameInput.Blur()
	for i := range d.pathInputs {
		d.pathInputs[i].Blur()
	}
	d.commandInput.Blur()
	d.branchInput.Blur()
	d.epicIDInput.Blur()
	d.claudeOptions.Blur()
	d.geminiOptions.Blur()
	d.codexOptions.Blur()

	// Manage soft-select: re-activate when entering path field with a value.
	d.pathSoftSelected = false
	switch d.currentTarget() {
	case focusName:
		d.nameInput.Focus()
	case focusPath:
		pi := d.activePathInput()
		if d.activePathIdx == 0 && pi.Value() != "" {
			d.pathSoftSelected = true
			// Keep pathInput blurred — we render custom reverse-video style.
			// pathInput.Focus() is called when soft-select exits.
		} else {
			pi.Focus()
		}
	case focusCommand:
		if d.commandCursor == 0 { // shell.
			d.commandInput.Focus()
		}
	case focusWorktree, focusSandbox, focusEpicRunner, focusInherited:
		// Checkbox/toggle rows — no text input to focus.
	case focusEpicID:
		d.epicIDInput.Focus()
	case focusBranch:
		d.branchInput.Focus()
	case focusOptions:
		if d.toolOptions != nil {
			d.toolOptions.Focus()
		}
	}
}

// Update handles key messages.
func (d *NewDialog) Update(msg tea.Msg) (*NewDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	var cmd tea.Cmd
	maxIdx := len(d.focusTargets) - 1
	cur := d.currentTarget()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Recent sessions picker handling
		if d.showRecentPicker && len(d.recentSessions) > 0 {
			switch msg.String() {
			case "ctrl+n", "down":
				d.recentSessionCursor = (d.recentSessionCursor + 1) % len(d.recentSessions)
				d.previewRecentSession(d.recentSessions[d.recentSessionCursor])
				return d, nil
			case "ctrl+p", "up":
				d.recentSessionCursor--
				if d.recentSessionCursor < 0 {
					d.recentSessionCursor = len(d.recentSessions) - 1
				}
				d.previewRecentSession(d.recentSessions[d.recentSessionCursor])
				return d, nil
			case "enter":
				// Fields already applied via preview — just close picker.
				d.showRecentPicker = false
				d.recentSnapshot = nil
				d.pathSoftSelected = true
				return d, nil
			case "esc", "ctrl+r":
				// Cancel — restore original form state.
				if d.recentSnapshot != nil {
					d.restoreSnapshot(d.recentSnapshot)
					d.recentSnapshot = nil
				}
				d.showRecentPicker = false
				return d, nil
			}
			return d, nil // Consume all other keys while picker is open
		}

		// Toggle recent sessions picker
		if msg.String() == "ctrl+r" && len(d.recentSessions) > 0 {
			d.recentSnapshot = d.saveSnapshot()
			d.showRecentPicker = true
			d.recentSessionCursor = 0
			d.previewRecentSession(d.recentSessions[0])
			return d, nil
		}

		// Soft-select interception for path field (only on first field)
		if d.currentTarget() == focusPath && d.pathSoftSelected && d.activePathIdx == 0 {
			pi := d.activePathInput()
			switch msg.Type {
			case tea.KeyRunes:
				// Printable char: clear field, focus textinput, let rune fall through
				d.pathSoftSelected = false
				pi.SetValue("")
				pi.SetCursor(0)
				pi.Focus()
				d.pathCycler.Reset()
				// DON'T return — let the rune reach textinput.Update() below
			case tea.KeyBackspace, tea.KeyDelete:
				d.pathSoftSelected = false
				pi.SetValue("")
				pi.SetCursor(0)
				pi.Focus()
				d.pathCycler.Reset()
				d.filterPathSuggestions()
				return d, nil // consume the key
			case tea.KeyLeft, tea.KeyRight:
				d.pathSoftSelected = false
				pi.Focus() // exit soft-select, allow editing
			}
			// Tab, Enter, Esc, Ctrl+N, Ctrl+P, Up, Down fall through to existing handlers
		}

		switch msg.String() {
		case "tab":
			// On path field: smart filesystem autocomplete.
			if cur == focusPath {
				pi := d.activePathInput()
				path := pi.Value()

				// If the cycler is already active, cycle to next match.
				if d.pathCycler.IsActive() {
					val := d.pathCycler.Next()
					pi.SetValue(val)
					pi.SetCursor(len(val))
					return d, nil
				}

				// Get filesystem completions for the current input.
				matches, err := session.GetDirectoryCompletions(path)
				if err == nil && len(matches) > 0 {
					if len(matches) == 1 {
						// Single match — complete and append / so next Tab drills in.
						completed := matches[0] + string(os.PathSeparator)
						pi.SetValue(completed)
						pi.SetCursor(len(completed))
						d.pathCycler.Reset()
					} else {
						// Multiple matches — start cycling.
						d.pathCycler.SetMatches(matches)
						val := d.pathCycler.Next()
						pi.SetValue(val)
						pi.SetCursor(len(val))
					}
					return d, nil
				}
			}

			// On path field: apply selected suggestion ONLY if user explicitly navigated.
			if cur == focusPath && d.suggestionNavigated && len(d.pathSuggestions) > 0 {
				pi := d.activePathInput()
				if d.pathSuggestionCursor < len(d.pathSuggestions) {
					pi.SetValue(d.pathSuggestions[d.pathSuggestionCursor])
					pi.SetCursor(len(pi.Value()))
				}
			}
			// Reset path cycler when tabbing away from the path field.
			if cur == focusPath {
				d.pathCycler.Reset()
				// If current path field has content, move to next path field (create if needed).
				pi := d.activePathInput()
				if strings.TrimSpace(pi.Value()) != "" {
					d.ensureEmptyTrailingPath()
					d.activePathIdx++
					d.pathSoftSelected = false
					d.updateFocus()
					return d, nil
				}
			}
			// Move to next field.
			if d.focusIndex < maxIdx {
				d.focusIndex++
				d.updateFocus()
			} else if cur == focusOptions && d.toolOptions != nil {
				return d, d.toolOptions.Update(msg)
			} else {
				d.focusIndex = 0
				d.updateFocus()
			}
			// Reset navigation flag when leaving path field.
			if d.currentTarget() != focusPath {
				d.suggestionNavigated = false
			}
			return d, cmd

		case "ctrl+n":
			// Next suggestion (when on path field).
			if cur == focusPath && len(d.pathSuggestions) > 0 {
				d.pathSoftSelected = false
				d.activePathInput().Focus() // exit soft-select, focus for future input.
				d.pathSuggestionCursor = (d.pathSuggestionCursor + 1) % len(d.pathSuggestions)
				d.suggestionNavigated = true
				return d, nil
			}

		case "ctrl+p":
			// Previous suggestion (when on path field).
			if cur == focusPath && len(d.pathSuggestions) > 0 {
				d.pathSoftSelected = false
				d.activePathInput().Focus() // exit soft-select, focus for future input.
				d.pathSuggestionCursor--
				if d.pathSuggestionCursor < 0 {
					d.pathSuggestionCursor = len(d.pathSuggestions) - 1
				}
				d.suggestionNavigated = true
				return d, nil
			}

		case "down":
			if cur == focusPath {
				pi := d.activePathInput()
				// If current path has content and we're on the last field, create a new one.
				if d.activePathIdx == len(d.pathInputs)-1 && strings.TrimSpace(pi.Value()) != "" {
					d.ensureEmptyTrailingPath()
				}
				if d.activePathIdx < len(d.pathInputs)-1 {
					d.activePathIdx++
					d.pathSoftSelected = false
					d.updateFocus()
					return d, nil
				}
			}
			if d.focusIndex < maxIdx {
				d.focusIndex++
				d.activePathIdx = 0
				d.trimEmptyTrailingPaths() // clean up when leaving path area
				d.updateFocus()
			} else if cur == focusOptions && d.toolOptions != nil {
				return d, d.toolOptions.Update(msg)
			}
			return d, nil

		case "shift+tab", "up":
			if cur == focusPath && d.activePathIdx > 0 {
				// Move to previous path field.
				d.activePathIdx--
				d.updateFocus()
				return d, nil
			}
			if cur == focusOptions && d.toolOptions != nil && !d.toolOptions.AtTop() {
				return d, d.toolOptions.Update(msg)
			}
			d.focusIndex--
			if d.focusIndex < 0 {
				d.focusIndex = maxIdx
			}
			d.updateFocus()
			return d, nil

		case "esc":
			d.Hide()
			return d, nil

		case "enter":
			return d, nil

		case "left":
			if cur == focusCommand {
				d.commandCursor--
				if d.commandCursor < 0 {
					d.commandCursor = len(d.presetCommands) - 1
				}
				d.updateToolOptions()
				d.updateFocus()
				return d, nil
			}
			if cur == focusOptions && d.toolOptions != nil {
				return d, d.toolOptions.Update(msg)
			}

		case "right":
			if cur == focusCommand {
				d.commandCursor = (d.commandCursor + 1) % len(d.presetCommands)
				d.updateToolOptions()
				d.updateFocus()
				return d, nil
			}
			if cur == focusOptions && d.toolOptions != nil {
				return d, d.toolOptions.Update(msg)
			}

		case "w":
			if cur == focusCommand {
				d.ToggleWorktree()
				d.rebuildFocusTargets()
				if d.worktreeEnabled {
					if idx := d.indexOf(focusBranch); idx >= 0 {
						d.focusIndex = idx
					}
					d.updateFocus()
				}
				return d, nil
			}

		case "s":
			if cur == focusCommand {
				d.ToggleSandbox()
				if !d.sandboxEnabled {
					d.inheritedExpanded = false
				}
				d.rebuildFocusTargets()
				return d, nil
			}

		case "e":
			if cur == focusCommand {
				d.ToggleEpicRunner()
				d.rebuildFocusTargets()
				if d.epicRunnerEnabled {
					if idx := d.indexOf(focusEpicID); idx >= 0 {
						d.focusIndex = idx
					}
					d.updateFocus()
				}
				return d, nil
			}

		case "y":
			selectedCmd := d.GetSelectedCommand()
			if cur == focusCommand && (selectedCmd == "gemini" || selectedCmd == "codex") && d.toolOptions != nil {
				d.toolOptions.Update(msg)
				return d, nil
			}
			if cur == focusOptions && d.toolOptions != nil {
				d.toolOptions.Update(msg)
				return d, nil
			}

		case " ":
			if cur == focusWorktree {
				d.ToggleWorktree()
				d.rebuildFocusTargets()
				if d.worktreeEnabled {
					if idx := d.indexOf(focusBranch); idx >= 0 {
						d.focusIndex = idx
					}
					d.updateFocus()
				}
				return d, nil
			}
			if cur == focusSandbox {
				d.ToggleSandbox()
				if !d.sandboxEnabled {
					d.inheritedExpanded = false
				}
				d.rebuildFocusTargets()
				return d, nil
			}
			if cur == focusEpicRunner {
				d.ToggleEpicRunner()
				d.rebuildFocusTargets()
				if d.epicRunnerEnabled {
					if idx := d.indexOf(focusEpicID); idx >= 0 {
						d.focusIndex = idx
					}
					d.updateFocus()
				}
				return d, nil
			}
			if cur == focusInherited {
				d.inheritedExpanded = !d.inheritedExpanded
				return d, nil
			}
			if cur == focusOptions && d.toolOptions != nil {
				return d, d.toolOptions.Update(msg)
			}
		}
	}

	// Update focused input.
	switch cur {
	case focusName:
		oldName := d.nameInput.Value()
		d.nameInput, cmd = d.nameInput.Update(msg)
		if d.worktreeEnabled && d.branchAutoSet && d.nameInput.Value() != oldName {
			d.autoBranchFromName()
		}
	case focusPath:
		pi := d.activePathInput()
		oldValue := pi.Value()
		*pi, cmd = pi.Update(msg)
		if pi.Value() != oldValue {
			d.suggestionNavigated = false
			d.pathSuggestionCursor = 0
			d.pathCycler.Reset()
			d.filterPathSuggestions()
			// Auto-grow: if user typed in the last field and it's non-empty, add a new empty field.
			if d.activePathIdx == len(d.pathInputs)-1 && strings.TrimSpace(pi.Value()) != "" {
				d.ensureEmptyTrailingPath()
			}
			// Auto-shrink: if field became empty via backspace and it's not the first field,
			// remove it and move focus up.
			if strings.TrimSpace(pi.Value()) == "" && d.activePathIdx > 0 && oldValue != "" {
				// Check if the msg was a backspace
				if keyMsg, ok := msg.(tea.KeyMsg); ok && (keyMsg.Type == tea.KeyBackspace || keyMsg.Type == tea.KeyDelete) {
					d.pathInputs = append(d.pathInputs[:d.activePathIdx], d.pathInputs[d.activePathIdx+1:]...)
					d.activePathIdx--
					d.activePathInput().Focus()
					d.activePathInput().SetCursor(len(d.activePathInput().Value()))
					return d, nil
				}
			}
		}
	case focusWorktree, focusSandbox, focusEpicRunner, focusInherited:
		// Checkbox/toggle rows — no text input to update.
	case focusEpicID:
		d.epicIDInput, cmd = d.epicIDInput.Update(msg)
	case focusBranch:
		oldBranch := d.branchInput.Value()
		d.branchInput, cmd = d.branchInput.Update(msg)
		if d.branchInput.Value() != oldBranch {
			d.branchAutoSet = false
		}
	case focusOptions:
		if d.toolOptions != nil {
			cmd = d.toolOptions.Update(msg)
		}
	}

	return d, cmd
}

// View renders the dialog.
func (d *NewDialog) View() string {
	if !d.visible {
		return ""
	}

	cur := d.currentTarget()

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorCyan).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(ColorText)

	// Responsive dialog width
	dialogWidth := 60
	if d.width > 0 && d.width < dialogWidth+10 {
		dialogWidth = d.width - 10
		if dialogWidth < 40 {
			dialogWidth = 40
		}
	}

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorCyan).
		Background(ColorSurface).
		Padding(2, 4).
		Width(dialogWidth)

	// Active field indicator style
	activeLabelStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true)

	// Build content
	var content strings.Builder

	// Title with parent group info
	content.WriteString(titleStyle.Render("New Session"))
	content.WriteString("\n")
	groupInfoStyle := lipgloss.NewStyle().Foreground(ColorPurple) // Purple for group context
	content.WriteString(groupInfoStyle.Render("  in group: " + d.parentGroupName))
	content.WriteString("\n")

	// Recent sessions picker
	if d.showRecentPicker && len(d.recentSessions) > 0 {
		pickerHeaderStyle := lipgloss.NewStyle().Foreground(ColorComment)
		pickerSelectedStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
		pickerItemStyle := lipgloss.NewStyle().Foreground(ColorComment)

		content.WriteString("\n")
		content.WriteString(pickerHeaderStyle.Render(
			fmt.Sprintf("─ Recent Sessions (%d) ─ ↑↓ navigate │ Enter apply │ Esc close ─", len(d.recentSessions)),
		))
		content.WriteString("\n")

		maxShow := 5
		total := len(d.recentSessions)
		startIdx := 0
		endIdx := total
		if total > maxShow {
			startIdx = d.recentSessionCursor - maxShow/2
			if startIdx < 0 {
				startIdx = 0
			}
			endIdx = startIdx + maxShow
			if endIdx > total {
				endIdx = total
				startIdx = endIdx - maxShow
			}
		}

		if startIdx > 0 {
			content.WriteString(pickerItemStyle.Render(fmt.Sprintf("    ↑ %d more above", startIdx)))
			content.WriteString("\n")
		}

		for i := startIdx; i < endIdx; i++ {
			rs := d.recentSessions[i]
			// Format: Name  (tool @ ~/shortened/path)
			shortPath := rs.ProjectPath
			if home, err := os.UserHomeDir(); err == nil {
				shortPath = strings.Replace(shortPath, home, "~", 1)
			}
			toolLabel := rs.Tool
			if toolLabel == "" {
				toolLabel = "shell"
			}
			entry := fmt.Sprintf("%s  (%s @ %s)", rs.Title, toolLabel, shortPath)

			if i == d.recentSessionCursor {
				content.WriteString(pickerSelectedStyle.Render("  ▶ " + entry))
			} else {
				content.WriteString(pickerItemStyle.Render("    " + entry))
			}
			content.WriteString("\n")
		}

		if endIdx < total {
			content.WriteString(pickerItemStyle.Render(fmt.Sprintf("    ↓ %d more below", total-endIdx)))
			content.WriteString("\n")
		}
	}
	content.WriteString("\n")

	// Name input
	if cur == focusName {
		content.WriteString(activeLabelStyle.Render("▶ Name:"))
	} else {
		content.WriteString(labelStyle.Render("  Name:"))
	}
	content.WriteString("\n")
	content.WriteString("  ")
	content.WriteString(d.nameInput.View())
	content.WriteString("\n\n")

	// Path input(s) — dynamic list of path fields.
	{
		pathLabel := "Path:"
		if len(d.pathInputs) > 1 {
			// Count non-empty paths.
			nonEmpty := 0
			for _, pi := range d.pathInputs {
				if strings.TrimSpace(pi.Value()) != "" {
					nonEmpty++
				}
			}
			if nonEmpty > 1 {
				pathLabel = fmt.Sprintf("Paths: (%d)", nonEmpty)
			}
		}
		if cur == focusPath {
			content.WriteString(activeLabelStyle.Render("▶ " + pathLabel))
		} else {
			content.WriteString(labelStyle.Render("  " + pathLabel))
		}
		content.WriteString("\n")

		dimStyle := lipgloss.NewStyle().Foreground(ColorComment)
		for i, pi := range d.pathInputs {
			isActive := cur == focusPath && i == d.activePathIdx
			content.WriteString("  ")
			if isActive && d.pathSoftSelected && d.activePathIdx == 0 && pi.Value() != "" {
				// Render path in "selected" style (reverse video)
				selStyle := lipgloss.NewStyle().
					Background(ColorAccent).
					Foreground(ColorBg)
				content.WriteString(selStyle.Render(pi.Value()))
			} else if isActive {
				content.WriteString(pi.View())
			} else {
				val := pi.Value()
				if val == "" {
					val = pi.Placeholder
				}
				content.WriteString(dimStyle.Render(val))
			}
			content.WriteString("\n")
		}
	}

	// Show path suggestions dropdown when path field is focused
	if cur == focusPath && len(d.pathSuggestions) > 0 {
		suggestionStyle := lipgloss.NewStyle().
			Foreground(ColorComment)
		selectedStyle := lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

		// Show up to 5 suggestions in a scrolling window around the cursor
		maxShow := 5
		total := len(d.pathSuggestions)

		// Calculate visible window that follows the cursor
		startIdx := 0
		endIdx := total // Start with all suggestions
		if total > maxShow {
			// Need scrolling - center the cursor in the window
			startIdx = d.pathSuggestionCursor - maxShow/2
			if startIdx < 0 {
				startIdx = 0
			}
			endIdx = startIdx + maxShow
			if endIdx > total {
				endIdx = total
				startIdx = endIdx - maxShow
			}
		}

		var headerText string
		if len(d.pathSuggestions) < len(d.allPathSuggestions) {
			headerText = fmt.Sprintf("─ recent paths (%d/%d matching, ^N/^P: cycle, Tab: accept) ─",
				len(d.pathSuggestions), len(d.allPathSuggestions))
		} else {
			headerText = "─ recent paths (^N/^P: cycle, Tab: accept) ─"
		}
		content.WriteString("  ")
		content.WriteString(lipgloss.NewStyle().Foreground(ColorComment).Render(headerText))
		content.WriteString("\n")

		// Show "more above" indicator
		if startIdx > 0 {
			content.WriteString(suggestionStyle.Render(fmt.Sprintf("    ↑ %d more above", startIdx)))
			content.WriteString("\n")
		}

		for i := startIdx; i < endIdx; i++ {
			style := suggestionStyle
			prefix := "    "
			if i == d.pathSuggestionCursor {
				style = selectedStyle
				prefix = "  ▶ "
			}
			content.WriteString(style.Render(prefix + d.pathSuggestions[i]))
			content.WriteString("\n")
		}

		// Show "more below" indicator
		if endIdx < total {
			content.WriteString(suggestionStyle.Render(fmt.Sprintf("    ↓ %d more below", total-endIdx)))
			content.WriteString("\n")
		}
	}

	content.WriteString("\n")

	// Command selection
	if cur == focusCommand {
		content.WriteString(activeLabelStyle.Render("▶ Command:"))
	} else {
		content.WriteString(labelStyle.Render("  Command:"))
	}
	content.WriteString("\n  ")

	// Render command options as consistent pill buttons
	var cmdButtons []string
	for i, cmd := range d.presetCommands {
		displayName := cmd
		if displayName == "" {
			displayName = "shell"
		}
		// Prepend icon for custom tools
		if icon := session.GetToolIcon(cmd); cmd != "" && icon != "" {
			// Only prepend for custom tools (not built-ins which are recognizable by name)
			if toolDef := session.GetToolDef(cmd); toolDef != nil && toolDef.Icon != "" {
				displayName = icon + " " + displayName
			}
		}

		var btnStyle lipgloss.Style
		if i == d.commandCursor {
			// Selected: bright background, bold (active pill)
			btnStyle = lipgloss.NewStyle().
				Foreground(ColorBg).
				Background(ColorAccent).
				Bold(true).
				Padding(0, 2)
		} else {
			// Unselected: subtle background pill (consistent style)
			btnStyle = lipgloss.NewStyle().
				Foreground(ColorTextDim).
				Background(ColorSurface).
				Padding(0, 2)
		}

		cmdButtons = append(cmdButtons, btnStyle.Render(displayName))
	}
	content.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, cmdButtons...))
	content.WriteString("\n\n")

	// Custom command input (only if shell is selected)
	if d.commandCursor == 0 {
		// Show active indicator when command field is focused
		if cur == focusCommand {
			content.WriteString(activeLabelStyle.Render("  ▸ Custom:"))
		} else {
			content.WriteString(labelStyle.Render("    Custom:"))
		}
		content.WriteString("\n    ")
		content.WriteString(d.commandInput.View())
		content.WriteString("\n\n")
	}

	// Worktree checkbox — individually focusable.
	worktreeLabel := "Create in worktree"
	if cur == focusCommand {
		worktreeLabel = "Create in worktree (w)"
	}
	content.WriteString(renderCheckboxLine(worktreeLabel, d.worktreeEnabled, cur == focusWorktree))

	// Docker sandbox checkbox — individually focusable.
	sandboxLabel := "Run in Docker sandbox"
	if cur == focusCommand {
		sandboxLabel = "Run in Docker sandbox (s)"
	}
	content.WriteString(renderCheckboxLine(sandboxLabel, d.sandboxEnabled, cur == focusSandbox))

	// Epic runner checkbox — individually focusable.
	epicRunnerLabel := "Enable epic runner"
	if cur == focusCommand {
		epicRunnerLabel = "Enable epic runner (e)"
	}
	content.WriteString(renderCheckboxLine(epicRunnerLabel, d.epicRunnerEnabled, cur == focusEpicRunner))

	// Epic ID input (only visible when epic runner is enabled).
	if d.epicRunnerEnabled {
		content.WriteString("\n")
		if cur == focusEpicID {
			content.WriteString(activeLabelStyle.Render("▶ Epic ID:"))
		} else {
			content.WriteString(labelStyle.Render("  Epic ID:"))
		}
		content.WriteString("\n")
		content.WriteString("  ")
		content.WriteString(d.epicIDInput.View())
		content.WriteString("\n")
	}

	// Inherited Docker settings (only visible when sandbox is enabled).
	if d.sandboxEnabled && len(d.inheritedSettings) > 0 {
		focused := cur == focusInherited
		dimStyle := lipgloss.NewStyle().Foreground(ColorComment)
		settingStyle := lipgloss.NewStyle().Foreground(ColorTextDim)

		// Render toggle line.
		arrow := "▸"
		if d.inheritedExpanded {
			arrow = "▾"
		}
		summary := fmt.Sprintf("%d active", len(d.inheritedSettings))
		toggleLine := fmt.Sprintf("%s Docker Settings (%s)", arrow, summary)
		if focused {
			content.WriteString(activeLabelStyle.Render("▶ " + toggleLine))
		} else {
			content.WriteString("  " + dimStyle.Render(toggleLine))
		}
		content.WriteString("\n")

		// Render expanded settings.
		if d.inheritedExpanded {
			for _, s := range d.inheritedSettings {
				content.WriteString(settingStyle.Render(fmt.Sprintf("    %s: %s", s.label, s.value)))
				content.WriteString("\n")
			}
		}
	} else if d.sandboxEnabled {
		// Sandbox enabled but all defaults — show informational line.
		dimStyle := lipgloss.NewStyle().Foreground(ColorComment)
		content.WriteString("  " + dimStyle.Render("Docker Settings (all defaults)"))
		content.WriteString("\n")
	}

	// Branch input (only visible when worktree is enabled).
	if d.worktreeEnabled {
		content.WriteString("\n")
		if cur == focusBranch {
			content.WriteString(activeLabelStyle.Render("▶ Branch:"))
		} else {
			content.WriteString(labelStyle.Render("  Branch:"))
		}
		content.WriteString("\n")
		content.WriteString("  ")
		content.WriteString(d.branchInput.View())
		content.WriteString("\n")
	}

	// Tool options panel
	if d.toolOptions != nil {
		content.WriteString("\n")
		content.WriteString(d.toolOptions.View())
	}

	// Inline validation error
	if d.validationErr != "" {
		errStyle := lipgloss.NewStyle().Foreground(ColorRed).Bold(true)
		content.WriteString("\n")
		content.WriteString(errStyle.Render("  ⚠ " + d.validationErr))
	}

	content.WriteString("\n")

	// Help text with better contrast
	helpStyle := lipgloss.NewStyle().
		Foreground(ColorComment). // Use consistent theme color
		MarginTop(1)
	recentPrefix := ""
	if len(d.recentSessions) > 0 {
		recentPrefix = "^R recent │ "
	}
	helpText := recentPrefix + "Tab next/accept │ ↑↓ navigate │ Enter create │ Esc cancel"
	if cur == focusPath {
		if d.pathSoftSelected {
			helpText = "Type to replace │ ←→ to edit │ ^N/^P recent │ Tab next │ Esc cancel"
		} else if len(d.pathInputs) > 1 {
			helpText = "Tab autocomplete │ ^N/^P recent │ ↑↓ paths │ Enter create │ Esc cancel"
		} else {
			helpText = "Tab autocomplete │ ^N/^P recent │ ↑↓ navigate │ Enter create │ Esc cancel"
		}
	} else if cur == focusCommand {
		selectedCmd := d.GetSelectedCommand()
		if selectedCmd == "gemini" || selectedCmd == "codex" {
			helpText = "←→ command │ w worktree │ s sandbox │ e epic │ y yolo │ Tab next │ Enter create │ Esc cancel"
		} else {
			helpText = "←→ command │ w worktree │ s sandbox │ e epic │ Tab next │ Enter create │ Esc cancel"
		}
	} else if cur == focusWorktree || cur == focusSandbox || cur == focusEpicRunner {
		helpText = "Space toggle │ ↑↓ navigate │ Enter create │ Esc cancel"
	} else if cur == focusInherited {
		helpText = "Space expand/collapse │ ↑↓ navigate │ Enter create │ Esc cancel"
	} else if cur == focusOptions && d.toolOptions != nil {
		helpText = "Space/y toggle │ ↑↓ navigate │ Enter create │ Esc cancel"
	}
	content.WriteString(helpStyle.Render(helpText))

	// Wrap in dialog box
	dialog := dialogStyle.Render(content.String())

	// Center the dialog
	return lipgloss.Place(
		d.width,
		d.height,
		lipgloss.Center,
		lipgloss.Center,
		dialog,
	)
}

