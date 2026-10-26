package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditHTTP(t *testing.T) {
	root, dir := testRoot(t)
	put(t, dir, "שלום file.txt", "before\n")
	h := testHandler(root)
	target := "/api/edit?" + url.Values{"path": {"שלום file.txt"}}.Encode()
	read := request(h, "GET", target)
	if read.Code != 200 {
		t.Fatalf("GET: %d %s", read.Code, read.Body.String())
	}
	var doc TextFile
	if err := json.Unmarshal(read.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"content": "after\n", "revision": doc.Revision})
	save := func(body []byte, origin, contentType, header, secret string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", target, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-Dashboard-Edit", header)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if secret != "" {
			req.SetBasicAuth("dashboard", secret)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		name                                string
		body                                []byte
		origin, contentType, header, secret string
		status                              int
	}{
		{"auth", payload, "", "application/json", "1", "", 401},
		{"cross origin", payload, "https://evil.example", "application/json", "1", "test-only-secret", 403},
		{"form", payload, "", "text/plain", "1", "test-only-secret", 415},
		{"missing header", payload, "", "application/json", "", "test-only-secret", 403},
		{"bad JSON", []byte("{"), "", "application/json", "1", "test-only-secret", 400},
		{"extra JSON", append(append([]byte{}, payload...), []byte(" {}")...), "", "application/json", "1", "test-only-secret", 400},
		{"missing revision", []byte(`{"content":"x"}`), "", "application/json", "1", "test-only-secret", 400},
		{"binary", []byte(`{"content":"a\u0000b","revision":"x"}`), "", "application/json", "1", "test-only-secret", 415},
		{"oversize text", []byte(`{"content":"` + strings.Repeat("a", maxEditBytes+1) + `","revision":"x"}`), "", "application/json", "1", "test-only-secret", 413},
		{"oversize body", []byte(strings.Repeat(" ", 6*maxEditBytes+4097)), "", "application/json", "1", "test-only-secret", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := save(tc.body, tc.origin, tc.contentType, tc.header, tc.secret)
			if w.Code != tc.status || !json.Valid(w.Body.Bytes()) {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
	w := save(payload, "http://example.com", "application/json; charset=utf-8", "1", "test-only-secret")
	if w.Code != 200 {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	if w := save(payload, "", "application/json", "1", "test-only-secret"); w.Code != 409 {
		t.Fatalf("stale: %d", w.Code)
	}
	if w := request(h, "GET", "/api/edit?path=..%2Fother"); w.Code != 400 {
		t.Fatalf("traversal: %d", w.Code)
	}
	if w := request(h, "POST", target); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
}

func TestExternalEditHTTP(t *testing.T) {
	root, dir := testRoot(t)
	name := "שלום #?.txt"
	put(t, dir, name, "text")
	h := testHandler(root)
	w := request(h, "GET", "/api/external?"+url.Values{"path": {name}}.Encode())
	if w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	var details struct {
		Host, Username, Path, URL string
		Port                      int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &details); err != nil {
		t.Fatal(err)
	}
	current, _ := user.Current()
	if details.Host != "127.0.0.1" || details.Port != 22 || details.Username != current.Username || details.Path != filepath.Join(dir, name) {
		t.Fatalf("details: %+v", details)
	}
	link, err := url.Parse(details.URL)
	if err != nil || link.Scheme != "sftp" || link.Path != details.Path || link.User.Username() != details.Username {
		t.Fatalf("bad SFTP link %q", details.URL)
	}
	for _, path := range []string{"../outside", "/etc/passwd"} {
		if w := request(h, "GET", "/api/external?"+url.Values{"path": {path}}.Encode()); w.Code != 400 {
			t.Fatalf("traversal: %d", w.Code)
		}
	}
}
