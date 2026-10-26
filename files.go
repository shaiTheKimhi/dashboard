package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

var errBadPath = errors.New("invalid relative path")
var errNotRegular = errors.New("not a regular file")

type FileEntry struct {
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	SizeBytes  *int64     `json:"size_bytes"`
	ModifiedAt *time.Time `json:"modified_at"`
}
type FileListing struct {
	Path      string      `json:"path"`
	Parent    *string     `json:"parent"`
	Entries   []FileEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

func validatePath(relative string) (string, error) {
	if len(relative) > 4096 || strings.ContainsRune(relative, 0) || filepath.IsAbs(relative) {
		return "", errBadPath
	}
	for _, part := range strings.Split(relative, "/") {
		if part == ".." {
			return "", errBadPath
		}
	}
	if relative == "" {
		return ".", nil
	}
	if !filepath.IsLocal(relative) {
		return "", errBadPath
	}
	return filepath.Clean(relative), nil
}
func openShared(root *os.Root, relative string) (*os.File, error) {
	name, err := validatePath(relative)
	if err != nil {
		return nil, err
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	// os.Root rejects escapes even during concurrent symlink replacement. Only
	// report stable API errors; never return the host path or raw syscall text.
	if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
		return nil, fs.ErrPermission
	}
	return f, err
}
func OpenDownload(root *os.Root, relative string) (*os.File, fs.FileInfo, error) {
	f, err := openShared(root, relative)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, errNotRegular
	}
	return f, info, nil
}
func ListFiles(root *os.Root, relative string) (FileListing, error) {
	clean, err := validatePath(relative)
	if err != nil {
		return FileListing{}, err
	}
	f, err := openShared(root, clean)
	if err != nil {
		return FileListing{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return FileListing{}, err
	}
	if !info.IsDir() {
		return FileListing{}, errNotRegular
	}
	list := FileListing{Path: clean, Entries: []FileEntry{}}
	if clean == "." {
		list.Path = ""
	} else {
		parent := path.Dir(clean)
		if parent == "." {
			parent = ""
		}
		list.Parent = &parent
	}
	entries, err := f.ReadDir(1001)
	if err != nil && err != io.EOF {
		return FileListing{}, err
	}
	if len(entries) > 1000 {
		entries = entries[:1000]
		list.Truncated = true
	}
	for _, entry := range entries {
		item := FileEntry{Name: entry.Name(), Kind: "unavailable"}
		// Stat via the root as well: DirEntry.Info would describe the link itself.
		stat, e := root.Stat(filepath.Join(clean, entry.Name()))
		if e == nil {
			stamp := stat.ModTime().UTC()
			item.ModifiedAt = &stamp
			if stat.IsDir() {
				item.Kind = "directory"
			} else if stat.Mode().IsRegular() {
				item.Kind = "file"
				item.SizeBytes = pointer(stat.Size())
			}
		}
		list.Entries = append(list.Entries, item)
	}
	sort.Slice(list.Entries, func(i, j int) bool {
		a, b := list.Entries[i], list.Entries[j]
		if (a.Kind == "directory") != (b.Kind == "directory") {
			return a.Kind == "directory"
		}
		return a.Name < b.Name
	})
	return list, nil
}
