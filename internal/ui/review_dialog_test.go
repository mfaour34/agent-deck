package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanMarkdownFiles(t *testing.T) {
	dir := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not md"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "docs", "plan.md"), []byte("# Plan"), 0o644)

	_ = os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "node_modules", "pkg.md"), []byte("skip"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".git", "config.md"), []byte("skip"), 0o644)

	files := scanMarkdownFiles(dir)

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
	if len(d.allFiles) != 3 {
		t.Fatalf("expected 3 files, got %d", len(d.allFiles))
	}
	if d.SelectedCount() != 0 {
		t.Fatal("should start with 0 selected")
	}

	d.selected[0] = true
	if d.SelectedCount() != 1 {
		t.Fatal("should have 1 selected")
	}

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
	dir := t.TempDir()

	d.Show(dir, "test", "id1", "group1", 80, 40)

	if d.err == nil {
		t.Fatal("should have error when no .md files found")
	}
}
