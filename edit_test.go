package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestTextEditing(t *testing.T) {
	root, dir := testRoot(t)
	put(t, dir, "nested/שלום.txt", "first\r\nsecond\r\n")
	path := filepath.Join(dir, "nested/שלום.txt")
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	doc, err := ReadText(root, "nested/שלום.txt")
	if err != nil || doc.Content != "first\r\nsecond\r\n" || doc.Revision == "" {
		t.Fatalf("read: %+v %v", doc, err)
	}
	saved, err := SaveText(root, doc.Path, doc.Revision, "edited שלום\nnext\n")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	after, _ := os.Stat(path)
	if string(content) != "edited שלום\r\nnext\r\n" || saved.Revision == doc.Revision || after.Mode().Perm() != 0640 || os.SameFile(before, after) {
		t.Fatalf("save not atomic/preserving: %q %+v %v", content, saved, after.Mode())
	}
	oldStat, newStat := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
	if oldStat.Uid != newStat.Uid || oldStat.Gid != newStat.Gid {
		t.Fatal("ownership changed")
	}
	if _, err := SaveText(root, doc.Path, doc.Revision, "stale"); !errors.Is(err, errEditConflict) {
		t.Fatalf("accepted stale revision: %v", err)
	}
	if _, err := SaveText(root, doc.Path, saved.Revision, ""); err != nil {
		t.Fatal(err)
	}
	empty, err := ReadText(root, doc.Path)
	if err != nil || empty.Content != "" {
		t.Fatal("empty file", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary files remain")
	}
}

func TestEditBoundariesAndLimits(t *testing.T) {
	root, dir := testRoot(t)
	outside := t.TempDir()
	put(t, outside, "sentinel", "outside")
	put(t, dir, "text", "original")
	put(t, dir, "binary", "a\x00b")
	put(t, dir, "invalid-utf8", "\xff")
	put(t, dir, "large", strings.Repeat("a", maxEditBytes+1))
	if err := os.Symlink("text", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(outside, "sentinel"), filepath.Join(dir, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../sentinel", "/etc/passwd", "link", "escape/sentinel", "hardlink", "fifo", "binary", "invalid-utf8", "large", "."} {
		t.Run(path, func(t *testing.T) {
			if _, err := ReadText(root, path); err == nil {
				t.Fatal("accepted unsupported file")
			}
			if _, err := SaveText(root, path, "revision", "changed"); err == nil {
				t.Fatal("accepted unsupported save")
			}
		})
	}
	doc, err := ReadText(root, "text")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"a\x00b", "\xff", strings.Repeat("a", maxEditBytes+1)} {
		if _, err := SaveText(root, "text", doc.Revision, text); err == nil {
			t.Fatal("accepted invalid content")
		}
	}
	if err := os.Chmod(filepath.Join(dir, "text"), 0400); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		current, _ := ReadText(root, "text")
		if _, err := SaveText(root, "text", current.Revision, "changed"); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("read-only save: %v", err)
		}
	}
	original, _ := os.ReadFile(filepath.Join(dir, "text"))
	sentinel, _ := os.ReadFile(filepath.Join(outside, "sentinel"))
	if string(original) != "original" || string(sentinel) != "outside" {
		t.Fatal("failed save modified original")
	}
}

func TestEditConcurrentAndExternalChanges(t *testing.T) {
	root, dir := testRoot(t)
	put(t, dir, "text", "initial")
	doc, _ := ReadText(root, "text")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, content := range []string{"one", "two"} {
		wg.Add(1)
		go func(content string) {
			defer wg.Done()
			_, err := SaveText(root, "text", doc.Revision, content)
			results <- err
		}(content)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, errEditConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	doc, _ = ReadText(root, "text")
	put(t, dir, "text", "external change")
	if _, err := SaveText(root, "text", doc.Revision, "stale"); !errors.Is(err, errEditConflict) {
		t.Fatalf("external change lost: %v", err)
	}
	doc, _ = ReadText(root, "text")
	if err := os.Rename(filepath.Join(dir, "text"), filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "text", doc.Content)
	if _, err := SaveText(root, "text", doc.Revision, "stale"); !errors.Is(err, errEditConflict) {
		t.Fatalf("replacement not detected: %v", err)
	}
}
