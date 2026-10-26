package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct{ ListenAddr, FileRoot, SecretSHA256, TLSCert, TLSKey, Username string }

func LoadConfig() (Config, error) {
	c := Config{ListenAddr: os.Getenv("DASHBOARD_LISTEN"), FileRoot: os.Getenv("DASHBOARD_FILE_ROOT"), SecretSHA256: os.Getenv("DASHBOARD_SECRET_SHA256"), TLSCert: os.Getenv("DASHBOARD_TLS_CERT"), TLSKey: os.Getenv("DASHBOARD_TLS_KEY"), Username: os.Getenv("DASHBOARD_USERNAME")}
	if c.Username == "" {
		c.Username = "dashboard"
	}
	if c.ListenAddr == "" {
		c.ListenAddr = "127.0.0.1:8080"
	}
	if c.FileRoot == "" {
		c.FileRoot = "/home/shai/dashboard-share"
	}
	return c, c.Validate()
}

func queryPath(r *http.Request) (string, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", errBadPath
	}
	values := q["path"]
	if len(values) > 1 {
		return "", errBadPath
	}
	value := ""
	if len(values) == 1 {
		value = values[0]
	}
	_, err = validatePath(value)
	return value, err
}
func fileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadPath), errors.Is(err, errNotRegular):
		writeError(w, 400, "invalid_path", "Choose a valid relative folder or regular file.")
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, 404, "not_found", "This file or folder no longer exists.")
	case errors.Is(err, fs.ErrPermission):
		writeError(w, 403, "access_denied", "This file or folder is not accessible.")
	default:
		writeError(w, 500, "internal_error", "The file operation could not be completed.")
	}
}
func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeError(w, 500, "internal_error", "The device reading could not be encoded.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(append(b, '\n'))
}
func NewHandler(cfg Config, metrics func() Snapshot, root *os.Root, assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	registerEditRoutes(mux, cfg, root)
	registerManageRoutes(mux, root)
	downloads := make(chan struct{}, 2)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/system", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, metrics()) })
	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		relative, err := queryPath(r)
		if err != nil {
			fileError(w, err)
			return
		}
		list, err := ListFiles(root, relative)
		if err != nil {
			fileError(w, err)
			return
		}
		writeJSON(w, list)
	})
	mux.HandleFunc("GET /api/download", func(w http.ResponseWriter, r *http.Request) {
		relative, err := queryPath(r)
		if err != nil {
			fileError(w, err)
			return
		}
		select {
		case downloads <- struct{}{}:
			defer func() { <-downloads }()
		default:
			writeError(w, 429, "download_limit", "Two downloads are already running. Try again shortly.")
			return
		}
		f, info, err := OpenDownload(root, relative)
		if err != nil {
			fileError(w, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(relative)}))
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
	serveAsset := func(w http.ResponseWriter, r *http.Request, name string) {
		b, err := fs.ReadFile(assets, name)
		if err != nil {
			writeError(w, 404, "not_found", "Page asset not found.")
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
	}
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/static/")
		if name != "app.js" && name != "style.css" {
			writeError(w, 404, "not_found", "Page asset not found.")
			return
		}
		serveAsset(w, r, name)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeError(w, 404, "not_found", "Page not found.")
			return
		}
		serveAsset(w, r, "index.html")
	})
	return requireAuth(cfg, mux)
}

func (c Config) Validate() error {
	if strings.ContainsAny(c.Username, ":\r\n\x00") {
		return errors.New("DASHBOARD_USERNAME cannot contain colons, line breaks or NULs")
	}
	host, port, err := net.SplitHostPort(c.ListenAddr)
	ip := net.ParseIP(host)
	p, pe := strconv.Atoi(port)
	if err != nil || pe != nil || p < 1 || p > 65535 || ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("DASHBOARD_LISTEN must specify an explicit numeric unicast IP and port")
	}
	d, err := hex.DecodeString(c.SecretSHA256)
	if err != nil || len(d) != sha256.Size {
		return errors.New("DASHBOARD_SECRET_SHA256 must contain a SHA-256 hex digest")
	}
	if !filepath.IsAbs(c.FileRoot) || filepath.Clean(c.FileRoot) == "/" {
		return errors.New("DASHBOARD_FILE_ROOT must be an absolute dedicated directory, not /")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("both TLS certificate and key are required")
	}
	if c.TLSCert == "" {
		if !ip.IsLoopback() {
			return errors.New("non-loopback listeners require TLS")
		}
		return nil
	}
	pair, err := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey)
	if err != nil {
		return errors.New("cannot load TLS certificate/key; check paths, permissions and matching PEM material")
	}
	now := time.Now()
	for _, der := range pair.Certificate {
		cert, err := x509.ParseCertificate(der)
		if err != nil || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			return errors.New("TLS certificate chain is invalid, expired or not yet valid")
		}
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func requireAuth(c Config, next http.Handler) http.Handler {
	expected, _ := hex.DecodeString(c.SecretSHA256)
	username := c.Username
	if username == "" {
		username = "dashboard"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		user, secret, ok := r.BasicAuth()
		digest := sha256.Sum256([]byte(secret))
		valid := subtle.ConstantTimeCompare(digest[:], expected)
		if !ok || user != username || valid != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Jetson dashboard", charset="UTF-8"`)
			writeError(w, 401, "unauthorized", "Sign in with your dashboard credentials.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
