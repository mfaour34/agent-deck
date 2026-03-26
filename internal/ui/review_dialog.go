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
	projectPath string
	sessionName string
	sessionID   string
	groupPath   string

	files       []string
	selected    map[int]bool
	cursor      int
	scrollOff   int
	filterBuf   string
	filterUntil time.Time
	err         error
	confirmed   bool
}

const reviewFilterTimeout = 1200 * time.Millisecond

func NewReviewFileDialog() *ReviewFileDialog {
	return &ReviewFileDialog{
		selected: make(map[int]bool),
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
	d.filterBuf = ""
	d.err = nil
	d.confirmed = false

	d.files = scanMarkdownFiles(projectPath)
	if len(d.files) == 0 {
		d.err = fmt.Errorf("no markdown files found in %s", projectPath)
	}
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
	for i, f := range d.files {
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
			return d, nil
		}
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
	max := d.height - 8
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
	b.WriteString(dimStyle.Render(fmt.Sprintf("%d selected of %d files", d.SelectedCount(), len(d.files))))
	b.WriteString("\n\n")

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
