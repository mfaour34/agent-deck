package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ReviewFileDialog presents a multi-select list of .md files for mdreview.
type ReviewFileDialog struct {
	visible     bool
	width       int
	height      int
	projectPath string
	sessionName string
	sessionID   string
	groupPath   string

	allFiles    []string       // all .md files (unfiltered)
	filtered    []int          // indices into allFiles matching the current filter
	selected    map[int]bool   // keyed by allFiles index (survives filter changes)
	cursor      int            // position within filtered list
	scrollOff   int
	searchInput textinput.Model
	searching   bool           // true when search input is focused
	err         error
	confirmed   bool
}

func NewReviewFileDialog() *ReviewFileDialog {
	ti := textinput.New()
	ti.Placeholder = "type to filter..."
	ti.CharLimit = 100
	return &ReviewFileDialog{
		selected:    make(map[int]bool),
		searchInput: ti,
	}
}

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
	d.searchInput.SetValue("")
	d.searchInput.Blur()
	d.searching = false
	d.err = nil
	d.confirmed = false

	d.allFiles = scanMarkdownFiles(projectPath)
	if len(d.allFiles) == 0 {
		d.err = fmt.Errorf("no markdown files found in %s", projectPath)
	}
	d.rebuildFiltered()
	d.visible = true
}

func (d *ReviewFileDialog) Hide() {
	d.visible = false
	d.confirmed = false
}

func (d *ReviewFileDialog) IsVisible() bool {
	return d.visible
}

func (d *ReviewFileDialog) IsConfirmed() bool {
	return d.confirmed
}

func (d *ReviewFileDialog) SelectedFiles() []string {
	var result []string
	for i, f := range d.allFiles {
		if d.selected[i] {
			result = append(result, filepath.Join(d.projectPath, f))
		}
	}
	return result
}

func (d *ReviewFileDialog) SelectedCount() int {
	return len(d.selected)
}

func (d *ReviewFileDialog) SessionName() string {
	return d.sessionName
}

func (d *ReviewFileDialog) SessionGroupPath() string {
	return d.groupPath
}

func (d *ReviewFileDialog) ProjectPath() string {
	return d.projectPath
}

// rebuildFiltered recomputes the filtered list from the current search query.
func (d *ReviewFileDialog) rebuildFiltered() {
	query := strings.ToLower(strings.TrimSpace(d.searchInput.Value()))
	d.filtered = nil
	for i, f := range d.allFiles {
		if query == "" || strings.Contains(strings.ToLower(f), query) {
			d.filtered = append(d.filtered, i)
		}
	}
}

// clampCursor ensures cursor and scroll are within bounds after filter changes.
func (d *ReviewFileDialog) clampCursor() {
	if len(d.filtered) == 0 {
		d.cursor = 0
		d.scrollOff = 0
		return
	}
	if d.cursor >= len(d.filtered) {
		d.cursor = len(d.filtered) - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
	maxVis := d.maxVisible()
	if d.cursor < d.scrollOff {
		d.scrollOff = d.cursor
	} else if d.cursor >= d.scrollOff+maxVis {
		d.scrollOff = d.cursor - maxVis + 1
	}
	if d.scrollOff < 0 {
		d.scrollOff = 0
	}
}

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
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
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

func (d *ReviewFileDialog) Update(msg tea.Msg) (*ReviewFileDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	// When search input is focused, route most keys to it.
	if d.searching {
		switch keyMsg.String() {
		case "esc":
			// Exit search mode (keep filter text)
			d.searching = false
			d.searchInput.Blur()
			return d, nil
		case "enter":
			// Exit search mode and stay on list
			d.searching = false
			d.searchInput.Blur()
			return d, nil
		default:
			oldVal := d.searchInput.Value()
			var cmd tea.Cmd
			d.searchInput, cmd = d.searchInput.Update(msg)
			if d.searchInput.Value() != oldVal {
				d.rebuildFiltered()
				d.cursor = 0
				d.scrollOff = 0
				d.clampCursor()
			}
			return d, cmd
		}
	}

	// Normal mode (list navigation)
	switch keyMsg.String() {
	case "esc":
		if d.searchInput.Value() != "" {
			// First esc clears the search filter
			d.searchInput.SetValue("")
			d.rebuildFiltered()
			d.cursor = 0
			d.scrollOff = 0
			d.clampCursor()
			return d, nil
		}
		d.Hide()
		return d, nil

	case "enter":
		if d.err != nil {
			d.Hide()
			return d, nil
		}
		if d.SelectedCount() == 0 {
			return d, nil
		}
		d.confirmed = true
		return d, nil

	case "/":
		d.searching = true
		d.searchInput.Focus()
		return d, nil

	case " ":
		if len(d.filtered) > 0 {
			realIdx := d.filtered[d.cursor]
			if d.selected[realIdx] {
				delete(d.selected, realIdx)
			} else {
				d.selected[realIdx] = true
			}
		}
		return d, nil

	case "a":
		// Toggle select all visible (filtered) / none
		allSelected := true
		for _, idx := range d.filtered {
			if !d.selected[idx] {
				allSelected = false
				break
			}
		}
		if allSelected {
			// Deselect all filtered
			for _, idx := range d.filtered {
				delete(d.selected, idx)
			}
		} else {
			for _, idx := range d.filtered {
				d.selected[idx] = true
			}
		}
		return d, nil

	case "j", "down":
		if d.cursor < len(d.filtered)-1 {
			d.cursor++
			maxVis := d.maxVisible()
			if d.cursor >= d.scrollOff+maxVis {
				d.scrollOff = d.cursor - maxVis + 1
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
		if len(d.filtered) > 0 {
			d.cursor = len(d.filtered) - 1
			maxVis := d.maxVisible()
			if d.cursor >= d.scrollOff+maxVis {
				d.scrollOff = d.cursor - maxVis + 1
			}
		}
		return d, nil
	}

	return d, nil
}

func (d *ReviewFileDialog) maxVisible() int {
	// Reserve lines for header, search bar, footer, border
	max := d.height - 10
	if max < 3 {
		max = 3
	}
	return max
}

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

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorCyan)
	b.WriteString(titleStyle.Render("Review Markdown Files"))
	b.WriteString("\n")

	if d.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(ColorRed)
		b.WriteString(errStyle.Render(d.err.Error()))
		b.WriteString("\n\n")
		dimStyle := lipgloss.NewStyle().Foreground(ColorComment)
		b.WriteString(dimStyle.Render("Press Enter or Esc to close"))
		return d.wrapInBox(b.String(), dialogWidth)
	}

	dimStyle := lipgloss.NewStyle().Foreground(ColorComment)

	// Search bar
	searchPrefix := "/"
	if d.searching {
		searchPrefix = lipgloss.NewStyle().Foreground(ColorCyan).Render("/")
	}
	b.WriteString(fmt.Sprintf("%s%s", searchPrefix, d.searchInput.View()))
	b.WriteString("\n")

	// Counter
	totalSelected := d.SelectedCount()
	if d.searchInput.Value() != "" {
		b.WriteString(dimStyle.Render(fmt.Sprintf("%d selected · %d/%d files matching", totalSelected, len(d.filtered), len(d.allFiles))))
	} else {
		b.WriteString(dimStyle.Render(fmt.Sprintf("%d selected of %d files", totalSelected, len(d.allFiles))))
	}
	b.WriteString("\n\n")

	// File list (from filtered indices)
	maxVisible := d.maxVisible()
	endIdx := d.scrollOff + maxVisible
	if endIdx > len(d.filtered) {
		endIdx = len(d.filtered)
	}

	selectedStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(ColorText)
	checkStyle := lipgloss.NewStyle().Foreground(ColorGreen)

	if len(d.filtered) == 0 {
		b.WriteString(dimStyle.Render("  No matching files"))
		b.WriteString("\n")
	} else {
		if d.scrollOff > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", d.scrollOff)))
			b.WriteString("\n")
		}

		for fi := d.scrollOff; fi < endIdx; fi++ {
			realIdx := d.filtered[fi]
			isCursor := fi == d.cursor
			isSelected := d.selected[realIdx]

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

			line := fmt.Sprintf("%s%s %s", prefix, check, d.allFiles[realIdx])
			if isCursor || isSelected {
				b.WriteString(style.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}

		if endIdx < len(d.filtered) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", len(d.filtered)-endIdx)))
			b.WriteString("\n")
		}
	}

	// Help line
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("[/: search, space: select, a: all, enter: confirm, esc: cancel]"))

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
