package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"
)

const maxEditBytes = 1 << 20

var (
	errEditConflict = errors.New("file changed since it was opened")
	errEditTooLarge = errors.New("text editing is limited to 1 MiB")
	errNotText      = errors.New("only UTF-8 text files can be edited")
	errEditLink     = errors.New("linked files cannot be edited in the browser")
	fileWriteMu     sync.Mutex
)

type TextFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

func checkText(data []byte) error {
	if len(data) > maxEditBytes {
		return errEditTooLarge
	}
	if !utf8.Valid(data) {
		return errNotText
	}
	for _, b := range data {
		if b < 32 && b != '\t' && b != '\n' && b != '\r' {
			return errNotText
		}
	}
	return nil
}

func openEditable(root *os.Root, name string, write bool) (*os.File, error) {
	parent, err := root.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	directory, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	flags := os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW
	if write {
		flags = os.O_RDWR | syscall.O_NONBLOCK | syscall.O_NOFOLLOW
	}
	// Root.OpenFile resolves symlinks itself, including with O_NOFOLLOW. Use
	// openat on the pinned parent for a strict no-follow final component.
	fd, err := syscall.Openat(int(directory.Fd()), filepath.Base(name), flags|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errEditLink
		}
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errNotRegular
	}
	if err == nil && info.Sys().(*syscall.Stat_t).Nlink != 1 {
		err = errEditLink
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func textRevision(info fs.FileInfo, content []byte) string {
	stat := info.Sys().(*syscall.Stat_t)
	hash := sha256.New()
	hash.Write(content)
	fmt.Fprintf(hash, "\x00%d:%d:%d:%d:%d:%d:%d:%v", stat.Dev, stat.Ino, stat.Mode, stat.Uid, stat.Gid, info.Size(), info.ModTime().UnixNano(), stat.Ctim)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func readEditable(f *os.File) ([]byte, fs.FileInfo, error) {
	before, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if before.Size() > maxEditBytes {
		return nil, nil, errEditTooLarge
	}
	content, err := io.ReadAll(io.LimitReader(f, maxEditBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if err := checkText(content); err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if textRevision(before, content) != textRevision(after, content) {
		return nil, nil, errEditConflict
	}
	return content, after, nil
}

func ReadText(root *os.Root, relative string) (TextFile, error) {
	name, err := validatePath(relative)
	if err != nil {
		return TextFile{}, err
	}
	f, err := openEditable(root, name, false)
	if err != nil {
		return TextFile{}, err
	}
	defer f.Close()
	content, info, err := readEditable(f)
	if err != nil {
		return TextFile{}, err
	}
	return TextFile{Path: name, Content: string(content), Revision: textRevision(info, content)}, nil
}

func SaveText(root *os.Root, relative, revision, content string) (TextFile, error) {
	if err := checkText([]byte(content)); err != nil {
		return TextFile{}, err
	}
	name, err := validatePath(relative)
	if err != nil {
		return TextFile{}, err
	}
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	// Pin the parent so the final rename cannot follow a replaced directory link.
	parent, err := root.OpenRoot(filepath.Dir(name))
	if err != nil {
		return TextFile{}, err
	}
	defer parent.Close()
	base := filepath.Base(name)
	f, err := openEditable(parent, base, true)
	if err != nil {
		return TextFile{}, err
	}
	defer f.Close()
	old, info, err := readEditable(f)
	if err != nil {
		return TextFile{}, err
	}
	if revision == "" || textRevision(info, old) != revision {
		return TextFile{}, errEditConflict
	}
	if bytes.Contains(old, []byte("\r\n")) && !bytes.Contains(bytes.ReplaceAll(old, []byte("\r\n"), nil), []byte("\n")) {
		content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\n", "\r\n")
	}
	if err := checkText([]byte(content)); err != nil {
		return TextFile{}, err
	}
	temporary := ".dashboard-edit-" + rand.Text()
	temp, err := parent.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return TextFile{}, err
	}
	defer temp.Close()
	defer parent.Remove(temporary)
	if _, err = io.WriteString(temp, content); err != nil {
		return TextFile{}, err
	}
	owner := info.Sys().(*syscall.Stat_t)
	if err = temp.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
		return TextFile{}, err
	}
	if err = temp.Chmod(info.Mode().Perm()); err != nil {
		return TextFile{}, err
	}
	if err = temp.Sync(); err != nil {
		return TextFile{}, err
	}
	// Detect replacements or writes since the initial revision check. External
	// writers do not share our lock; this final pre-rename check is best-effort.
	current, err := ReadText(parent, base)
	if err != nil {
		return TextFile{}, err
	}
	if current.Revision != revision {
		return TextFile{}, errEditConflict
	}
	if err = parent.Rename(temporary, base); err != nil {
		return TextFile{}, err
	}
	savedInfo, err := temp.Stat()
	if err != nil {
		return TextFile{}, err
	}
	return TextFile{Path: name, Content: content, Revision: textRevision(savedInfo, []byte(content))}, nil
}
