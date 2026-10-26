package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateAndDeleteEntries(t *testing.T) {
	root, dir := testRoot(t)
	folder, err := CreateEntry(root, "שלום folder", "directory")
	if err != nil || folder.Kind != "directory" {
		t.Fatalf("create directory: %+v %v", folder, err)
	}
	file, err := CreateEntry(root, "שלום folder/new #.txt", "file")
	if err != nil || file.Kind != "file" {
		t.Fatalf("create file: %+v %v", file, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, file.Path))
	if err != nil || len(data) != 0 {
		t.Fatalf("file content: %q %v", data, err)
	}
	info, _ := os.Stat(filepath.Join(dir, file.Path))
	if info.Mode().Perm() != 0600 {
		t.Fatal("file not private", info.Mode())
	}
	if _, err := CreateEntry(root, file.Path, "file"); !errors.Is(err, fs.ErrExist) {
		t.Fatal("duplicate file", err)
	}
	if _, err := CreateEntry(root, folder.Path, "file"); !errors.Is(err, fs.ErrExist) {
		t.Fatal("replaced folder", err)
	}
	if err := DeleteEntry(root, file.Path, file.Revision, "wrong"); err == nil {
		t.Fatal("accepted wrong confirmation name")
	}
	if err := DeleteEntry(root, file.Path, file.Revision, file.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, file.Path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("file remains", err)
	}
	put(t, dir, "שלום folder/nested/.hidden", "contents")
	folder, err = InspectEntry(root, folder.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteEntry(root, folder.Path, folder.Revision, folder.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, folder.Path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("recursive folder remains", err)
	}
	if _, err := CreateEntry(root, ".hidden", "file"); err != nil {
		t.Fatal(err)
	}
}

func TestEntryBoundariesAndLinks(t *testing.T) {
	root, dir := testRoot(t)
	outside := t.TempDir()
	put(t, outside, "sentinel", "outside")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(outside, "sentinel"), filepath.Join(dir, "hardlink")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "./", "x/..", "../sentinel", "/etc/passwd", "escape/sentinel", "escape/new", "x\x00y"} {
		t.Run(name, func(t *testing.T) {
			if _, err := CreateEntry(root, name, "file"); err == nil {
				t.Fatal("accepted unsafe create")
			}
			if _, err := InspectEntry(root, name); err == nil {
				t.Fatal("accepted unsafe preview")
			}
			if err := DeleteEntry(root, name, "revision", "sentinel"); err == nil {
				t.Fatal("accepted unsafe delete")
			}
		})
	}
	for _, name := range []string{"new/", "new/.", "line\nname", "\xff", "   ", strings.Repeat("x", 256)} {
		if _, err := CreateEntry(root, name, "file"); err == nil {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
	if _, err := CreateEntry(root, "file", "unsupported"); err == nil {
		t.Fatal("accepted invalid kind")
	}
	for _, name := range []string{"escape", "dangling", "hardlink"} {
		if _, err := CreateEntry(root, name, "file"); !errors.Is(err, fs.ErrExist) {
			t.Fatalf("creation replaced link %s: %v", name, err)
		}
		entry, err := InspectEntry(root, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := DeleteEntry(root, name, entry.Revision, entry.Name); err != nil {
			t.Fatal(err)
		}
	}
	sentinel, err := os.ReadFile(filepath.Join(outside, "sentinel"))
	if err != nil || string(sentinel) != "outside" {
		t.Fatal("changed link target", err)
	}
}

func TestEntryStaleConfirmationsAndPermissions(t *testing.T) {
	root, dir := testRoot(t)
	put(t, dir, "file", "before")
	entry, err := InspectEntry(root, "file")
	if err != nil {
		t.Fatal(err)
	}
	put(t, dir, "file", "changed")
	if err := DeleteEntry(root, "file", entry.Revision, entry.Name); !errors.Is(err, errEditConflict) {
		t.Fatal("stale file deleted", err)
	}
	current, _ := InspectEntry(root, "file")
	if err := os.Rename(filepath.Join(dir, "file"), filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "file", "changed")
	if err := DeleteEntry(root, "file", current.Revision, current.Name); !errors.Is(err, errEditConflict) {
		t.Fatal("replacement deleted", err)
	}
	if _, err := CreateEntry(root, "private", "directory"); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "private/file", "safe")
	current, _ = InspectEntry(root, "private/file")
	if err := os.Chmod(filepath.Join(dir, "private"), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "private"), 0700) })
	if os.Geteuid() != 0 {
		if _, err := CreateEntry(root, "private/new", "file"); !errors.Is(err, fs.ErrPermission) {
			t.Fatal("write to read-only directory", err)
		}
		if err := DeleteEntry(root, current.Path, current.Revision, current.Name); !errors.Is(err, fs.ErrPermission) {
			t.Fatal("delete in read-only directory", err)
		}
	}
}
