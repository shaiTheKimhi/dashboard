package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Catches encoding failures being returned as empty successful API responses.
func TestUnencodableJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]float64{"reading": math.NaN()})
	if w.Code != 500 || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("invalid JSON response: %d %q", w.Code, w.Body.String())
	}
}

func testConfig() Config {
	d := sha256.Sum256([]byte("test-only-secret"))
	return Config{ListenAddr: "127.0.0.1:8080", FileRoot: "/tmp", SecretSHA256: hex.EncodeToString(d[:])}
}

func testCertificate(t *testing.T, from, until time.Time) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: from, NotAfter: until, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	k := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0600); err != nil {
		t.Fatal(err)
	}
	return cert, k
}

// Catches accidentally exposing plaintext or accepting unusable configuration.
func TestValidateConfig(t *testing.T) {
	cert, key := testCertificate(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	for _, tc := range []struct {
		name, listen, digest, cert, key string
		ok                              bool
	}{
		{"loopback", "127.0.0.1:8080", testConfig().SecretSHA256, "", "", true},
		{"ipv6", "[::1]:8080", testConfig().SecretSHA256, "", "", true},
		{"LAN plaintext", "192.168.68.107:8443", testConfig().SecretSHA256, "", "", false},
		{"all interfaces", ":8443", testConfig().SecretSHA256, cert, key, false},
		{"unspecified", "0.0.0.0:8443", testConfig().SecretSHA256, cert, key, false},
		{"hostname", "localhost:8080", testConfig().SecretSHA256, "", "", false},
		{"bad port", "127.0.0.1:0", testConfig().SecretSHA256, "", "", false},
		{"missing secret", "127.0.0.1:8080", "", "", "", false},
		{"invalid secret", "127.0.0.1:8080", strings.Repeat("z", 64), "", "", false},
		{"LAN TLS", "192.168.68.107:8443", testConfig().SecretSHA256, cert, key, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig()
			c.ListenAddr = tc.listen
			c.SecretSHA256 = tc.digest
			c.TLSCert = tc.cert
			c.TLSKey = tc.key
			if err := c.Validate(); (err == nil) != tc.ok {
				t.Fatalf("Validate()=%v, want ok=%v", err, tc.ok)
			}
		})
	}
}

// Catches certificate checks that allow expired, future or mismatched material.
func TestTLSConfig(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name        string
		from, until time.Time
	}{
		{"expired", now.Add(-2 * time.Hour), now.Add(-time.Hour)},
		{"future", now.Add(time.Hour), now.Add(2 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig()
			c.TLSCert, c.TLSKey = testCertificate(t, tc.from, tc.until)
			if c.Validate() == nil {
				t.Fatal("accepted invalid certificate lifetime")
			}
		})
	}
	c := testConfig()
	c.TLSCert, c.TLSKey = testCertificate(t, now.Add(-time.Hour), now.Add(time.Hour))
	_, otherKey := testCertificate(t, now.Add(-time.Hour), now.Add(time.Hour))
	c.TLSKey = otherKey
	if c.Validate() == nil {
		t.Fatal("accepted mismatched key")
	}
	c.TLSKey = ""
	if c.Validate() == nil {
		t.Fatal("accepted missing key")
	}
	c.TLSCert = "/nonexistent"
	c.TLSKey = "/nonexistent"
	if c.Validate() == nil {
		t.Fatal("accepted nonexistent TLS files")
	}
}

// Catches auth middleware being bypassed for static, health or unknown routes.
func TestRequireAuth(t *testing.T) {
	h := requireAuth(testConfig(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, path := range []string{"/healthz", "/static/app.js", "/missing", "/api/files"} {
		for _, secret := range []string{"", "wrong", "test-only-secret"} {
			req := httptest.NewRequest("GET", path, nil)
			if secret != "" {
				req.SetBasicAuth("dashboard", secret)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			want := 401
			if secret == "test-only-secret" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("%s auth=%q: %d", path, secret, w.Code)
			}
			if want == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing Basic challenge")
			}
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("someone-else", "test-only-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("wrong user accepted")
	}
	req.Header.Set("Authorization", "Basic !invalid")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("malformed auth accepted")
	}
}

// Catches changed login names being ignored while the password is updated.
func TestConfiguredUsername(t *testing.T) {
	c := testConfig()
	c.Username = "custom-test-user"
	h := requireAuth(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, tc := range []struct {
		user, secret string
		status       int
	}{
		{"custom-test-user", "test-only-secret", 200},
		{"dashboard", "test-only-secret", 401},
		{"custom-test-user", "wrong", 401},
	} {
		r := httptest.NewRequest("GET", "/healthz", nil)
		r.SetBasicAuth(tc.user, tc.secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("login status %d, want %d", w.Code, tc.status)
		}
	}
	t.Setenv("DASHBOARD_LISTEN", c.ListenAddr)
	t.Setenv("DASHBOARD_FILE_ROOT", c.FileRoot)
	t.Setenv("DASHBOARD_SECRET_SHA256", c.SecretSHA256)
	t.Setenv("DASHBOARD_TLS_CERT", "")
	t.Setenv("DASHBOARD_TLS_KEY", "")
	t.Setenv("DASHBOARD_USERNAME", "custom-test-user")
	loaded, err := LoadConfig()
	if err != nil || loaded.Username != "custom-test-user" {
		t.Fatalf("configured login was not loaded: %v", err)
	}
	t.Setenv("DASHBOARD_USERNAME", "")
	loaded, err = LoadConfig()
	if err != nil || loaded.Username != "dashboard" {
		t.Fatalf("default login was not loaded: %v", err)
	}
	for _, name := range []string{"bad:name", "line\nbreak", "null\x00name"} {
		c.Username = name
		if c.Validate() == nil {
			t.Fatal("invalid username accepted")
		}
	}
}
