package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mutationRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.SetBasicAuth("dashboard", "test-only-secret")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Dashboard-Edit", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestManageHTTP(t *testing.T) {
	root, dir := testRoot(t)
	h := testHandler(root)
	target := "/api/entry?" + url.Values{"path": {"שלום #.txt"}}.Encode()
	w := mutationRequest(h, "POST", target, `{"kind":"file"}`)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	if w := mutationRequest(h, "POST", target, `{"kind":"directory"}`); w.Code != 409 {
		t.Fatalf("collision %d", w.Code)
	}
	w = request(h, "GET", target)
	var entry EntryInfo
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &entry) != nil || entry.Kind != "file" || entry.Revision == "" {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]string{"revision": entry.Revision, "name": entry.Name})
	if w := mutationRequest(h, "DELETE", target, `{"revision":"x","name":"wrong"}`); w.Code != 400 {
		t.Fatalf("confirmation %d", w.Code)
	}
	if w := mutationRequest(h, "DELETE", target, strings.Replace(string(body), entry.Revision, "stale", 1)); w.Code != 409 {
		t.Fatalf("conflict %d", w.Code)
	}
	if w := mutationRequest(h, "DELETE", target, string(body)); w.Code != 200 {
		t.Fatalf("delete %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, entry.Name)); !os.IsNotExist(err) {
		t.Fatal("file remains")
	}
	for _, method := range []string{"POST", "GET", "DELETE"} {
		if w := mutationRequest(h, method, "/api/entry?path=.", `{"kind":"file","name":".","revision":"x"}`); w.Code == 200 || w.Code == 201 {
			t.Fatalf("accepted root %s", method)
		}
	}
}

func TestManageHTTPWriteProtection(t *testing.T) {
	root, dir := testRoot(t)
	put(t, dir, "file", "preserve")
	h := testHandler(root)
	for _, method := range []string{"POST", "DELETE"} {
		for _, tc := range []struct {
			name, body, origin, header, ctype string
			auth                              bool
			status                            int
		}{
			{"unauth", `{"kind":"file"}`, "", "1", "application/json", false, 401},
			{"origin", `{"kind":"file"}`, "https://other.example", "1", "application/json", true, 403},
			{"header", `{"kind":"file"}`, "", "", "application/json", true, 403},
			{"form", `{"kind":"file"}`, "", "1", "text/plain", true, 415},
			{"bad json", "{", "", "1", "application/json", true, 400},
			{"extra json", "{} {}", "", "1", "application/json", true, 400},
			{"unknown field", `{"unknown":1}`, "", "1", "application/json", true, 400},
			{"oversize", strings.Repeat(" ", 4097), "", "1", "application/json", true, 413},
			{"missing fields", "{}", "", "1", "application/json", true, 400},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				r := httptest.NewRequest(method, "/api/entry?path=file", strings.NewReader(tc.body))
				if tc.auth {
					r.SetBasicAuth("dashboard", "test-only-secret")
				}
				r.Header.Set("Content-Type", tc.ctype)
				r.Header.Set("X-Dashboard-Edit", tc.header)
				r.Header.Set("Origin", tc.origin)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != tc.status || !json.Valid(w.Body.Bytes()) {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
			})
		}
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		r := httptest.NewRequest(method, "/api/entry?path=file", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unauth %s %d", method, w.Code)
		}
	}
	content, _ := os.ReadFile(filepath.Join(dir, "file"))
	if string(content) != "preserve" {
		t.Fatal("rejected request changed file")
	}
}
