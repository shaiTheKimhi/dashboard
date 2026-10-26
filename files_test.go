package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

func testRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, dir
}
func testHandler(root *os.Root) http.Handler {
	return NewHandler(testConfig(), func() Snapshot { return Snapshot{Warnings: []string{}} }, root, fstest.MapFS{"index.html": {Data: []byte("dashboard")}, "app.js": {Data: []byte("test asset")}})
}
func request(h http.Handler, method, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.SetBasicAuth("dashboard", "test-only-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Catches unbounded enumeration, incorrect navigation and loss of literal filenames.
func TestListFiles(t *testing.T) {
	root, dir := testRoot(t)
	put(t, dir, "z.txt", "abc")
	put(t, dir, ".hidden", "shared")
	put(t, dir, "<b> שלום.txt", "hello")
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	list, err := ListFiles(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if list.Parent != nil || len(list.Entries) != 4 || list.Entries[0].Name != "folder" || list.Entries[0].Kind != "directory" {
		t.Fatalf("bad listing %+v", list)
	}
	empty, err := ListFiles(root, "folder")
	if err != nil || len(empty.Entries) != 0 || empty.Parent == nil || *empty.Parent != "" {
		t.Fatalf("bad empty listing %+v %v", empty, err)
	}
	for i := 0; i < 1001; i++ {
		put(t, dir, fmt.Sprintf("many/f%04d", i), "x")
	}
	many, err := ListFiles(root, "many")
	if err != nil || len(many.Entries) != 1000 || !many.Truncated {
		t.Fatal("missing bounded listing/truncated flag", err)
	}
	if _, err := ListFiles(root, "z.txt"); err == nil {
		t.Fatal("listed regular file as directory")
	}
}

// Catches validation mistakes and rooted filesystem escape through links or races.
func TestFileBoundary(t *testing.T) {
	root, dir := testRoot(t)
	outside := t.TempDir()
	put(t, outside, "secret", "outside sentinel")
	put(t, dir, "safe", "inside")
	put(t, dir, ".hidden", "shared")
	for name, target := range map[string]string{"escape": outside, "absolute": filepath.Join(dir, "safe"), "inside": "safe"} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/etc/passwd", "../secret", "x/../safe", "a\x00b", strings.Repeat("a", 4097), "escape/secret", "absolute"} {
		if f, _, err := OpenDownload(root, path); err == nil {
			f.Close()
			t.Fatalf("accepted forbidden path %q", path)
		}
	}
	for _, path := range []string{"inside", ".hidden"} {
		f, _, err := OpenDownload(root, path)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	h := testHandler(root)
	for _, query := range []string{"path=%zz", "path=safe&path=escape/secret", "path=%00", "path=..%2Fsecret"} {
		if w := request(h, "GET", "/api/download?"+query); w.Code != 400 {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	w := request(h, "GET", "/api/download?path=%252e%252e%252fsecret")
	if w.Code == 200 || strings.Contains(w.Body.String(), "outside sentinel") {
		t.Fatal("double decoding escape")
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Symlink(outside, filepath.Join(dir, "replacement"))
			_ = os.Rename(filepath.Join(dir, "replacement"), filepath.Join(dir, "race"))
			_ = os.Symlink(".", filepath.Join(dir, "replacement"))
			_ = os.Rename(filepath.Join(dir, "replacement"), filepath.Join(dir, "race"))
		}
	}()
	for i := 0; i < 200; i++ {
		f, _, err := OpenDownload(root, "race/secret")
		if err == nil {
			b, _ := io.ReadAll(f)
			f.Close()
			if strings.Contains(string(b), "outside sentinel") {
				close(stop)
				wg.Wait()
				t.Fatal("escaped during race")
			}
		}
	}
	close(stop)
	wg.Wait()
}

// Catches buffered downloads, content rendering, special-file blocking and bad HTTP semantics.
func TestDownload(t *testing.T) {
	root, dir := testRoot(t)
	put(t, dir, "page.html", "<html>hello</html>")
	put(t, dir, "שלום file.txt", "unicode")
	h := testHandler(root)
	w := request(h, "GET", "/api/download?path=page.html")
	if w.Code != 200 || w.Body.String() != "<html>hello</html>" || w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("unsafe download %d %v", w.Code, w.Header())
	}
	w = request(h, "HEAD", "/api/download?path=page.html")
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "18" {
		t.Fatalf("bad HEAD: %d len %d headers %v", w.Code, w.Body.Len(), w.Header())
	}
	req := httptest.NewRequest("GET", "/api/download?path=page.html", nil)
	req.SetBasicAuth("dashboard", "test-only-secret")
	req.Header.Set("Range", "bytes=0-5")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 206 || w.Body.String() != "<html>" {
		t.Fatalf("bad range: %d %q", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/download?path=%D7%A9%D7%9C%D7%95%D7%9D+file.txt")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "filename*") {
		t.Fatal("Unicode attachment filename lost")
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, _, err := OpenDownload(root, "pipe")
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked")
	}
	if _, _, err := OpenDownload(root, ""); err == nil {
		t.Fatal("directory accepted")
	}
	_ = os.Remove(filepath.Join(dir, "page.html"))
	if w = request(h, "GET", "/api/download?path=page.html"); w.Code != 404 {
		t.Fatal("missing file should be 404")
	}
	put(t, dir, "private", "private")
	if err := os.Chmod(filepath.Join(dir, "private"), 0000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if f, _, err := OpenDownload(root, "private"); err == nil {
			f.Close()
			t.Fatal("permission denied ignored")
		}
	}
	if w = request(h, "POST", "/api/files"); w.Code != 405 {
		t.Fatal("write method accepted")
	}
	if w = request(h, "GET", "/static/../go.mod"); w.Code == 200 {
		t.Fatal("non-asset exposed")
	}
}

type blockedWriter struct {
	header  http.Header
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (w *blockedWriter) Header() http.Header { return w.header }
func (w *blockedWriter) WriteHeader(int)     {}
func (w *blockedWriter) Write(b []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return 0, context.Canceled
}

// Catches admission limits being checked after streaming and leaked slots on cancellation.
func TestConcurrentDownloads(t *testing.T) {
	root, dir := testRoot(t)
	if err := os.WriteFile(filepath.Join(dir, "large"), bytes.Repeat([]byte("x"), 1<<20), 0600); err != nil {
		t.Fatal(err)
	}
	h := testHandler(root)
	release := make(chan struct{})
	defer close(release)
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		w := &blockedWriter{header: make(http.Header), entered: make(chan struct{}), release: release}
		go func() {
			r := httptest.NewRequest("GET", "/api/download?path=large", nil)
			r.SetBasicAuth("dashboard", "test-only-secret")
			h.ServeHTTP(w, r)
			done <- struct{}{}
		}()
		select {
		case <-w.entered:
		case <-time.After(time.Second):
			t.Fatal("download never started")
		}
	}
	if w := request(h, "GET", "/api/download?path=large"); w.Code != 429 {
		t.Fatalf("third download got %d", w.Code)
	}
	if w := request(h, "GET", "/api/system"); w.Code != 200 {
		t.Fatal("downloads blocked metrics")
	}
	// Unblock the disconnected writers and verify their admission slots return.
	release <- struct{}{}
	release <- struct{}{}
	<-done
	<-done
	if w := request(h, "HEAD", "/api/download?path=large"); w.Code != 200 {
		t.Fatalf("slot leaked: %d", w.Code)
	}
}

var _ fs.FS = fstest.MapFS{}
