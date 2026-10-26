package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

var errEntryConfirmation = errors.New("entry name confirmation does not match")

type EntryInfo struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Revision string `json:"revision"`
}

func entryParent(root *os.Root, relative string) (*os.Root, string, string, error) {
	name, err := validatePath(relative)
	if err != nil || name == "." {
		return nil, "", "", errBadPath
	}
	parent, err := root.OpenRoot(filepath.Dir(name))
	if err != nil {
		if errors.Is(err, syscall.ENOTDIR) {
			err = errBadPath
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
			err = fs.ErrPermission
		}
		return nil, "", "", err
	}
	return parent, filepath.Base(name), name, nil
}

func describeEntry(parent *os.Root, base, path string) (EntryInfo, error) {
	info, err := parent.Lstat(base)
	if err != nil {
		return EntryInfo{}, err
	}
	kind := "other"
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		kind = "symlink"
	case info.IsDir():
		kind = "directory"
	case info.Mode().IsRegular():
		kind = "file"
	}
	return EntryInfo{Path: path, Name: base, Kind: kind, Revision: textRevision(info, nil)}, nil
}

func InspectEntry(root *os.Root, relative string) (EntryInfo, error) {
	parent, base, path, err := entryParent(root, relative)
	if err != nil {
		return EntryInfo{}, err
	}
	defer parent.Close()
	return describeEntry(parent, base, path)
}

func CreateEntry(root *os.Root, relative, kind string) (EntryInfo, error) {
	if kind != "file" && kind != "directory" {
		return EntryInfo{}, errBadPath
	}
	leaf := relative[strings.LastIndex(relative, "/")+1:]
	if leaf == "" || leaf == "." || leaf == ".." || len(leaf) > 255 || strings.TrimSpace(leaf) == "" || !utf8.ValidString(leaf) || strings.ContainsFunc(leaf, unicode.IsControl) {
		return EntryInfo{}, errBadPath
	}
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	parent, base, path, err := entryParent(root, relative)
	if err != nil {
		return EntryInfo{}, err
	}
	defer parent.Close()
	if kind == "directory" {
		err = parent.Mkdir(base, 0700)
	} else {
		var file *os.File
		file, err = parent.OpenFile(base, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			err = file.Close()
		}
	}
	if err != nil {
		return EntryInfo{}, err
	}
	return describeEntry(parent, base, path)
}

func DeleteEntry(root *os.Root, relative, revision, confirmation string) error {
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	parent, base, path, err := entryParent(root, relative)
	if err != nil {
		return err
	}
	defer parent.Close()
	if confirmation != base {
		return errEntryConfirmation
	}
	current, err := describeEntry(parent, base, path)
	if err != nil {
		return err
	}
	if revision == "" || current.Revision != revision {
		return errEditConflict
	}
	// RemoveAll removes a final symlink itself and never traverses its target.
	// As with saves, unrelated external processes do not share our write lock.
	return parent.RemoveAll(base)
}
