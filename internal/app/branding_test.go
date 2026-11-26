package app

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// 1x1 transparent PNG used as a stand-in for an administrator upload.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func pngBytes(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyPNG)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return data
}

// uploadBranding posts a file as multipart/form-data, the format the branding
// handler expects.
func uploadBranding(t *testing.T, c *client, url, filename string, data []byte) int {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.csr != "" {
		req.Header.Set("X-CSRF-Token", c.csr)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// Without an upload the branding endpoints and the static icon paths must serve
// the bundled TEX Share images instead of 404 or the SPA HTML.
func TestBrandingServesBundledDefaults(t *testing.T) {
	env := newTestEnv(t)
	// Public routes are gated until the first-run wizard completes.
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)
	anon := newClient(t)

	for _, kind := range []string{"logo", "favicon"} {
		resp, body := anon.do("GET", env.public.URL+"/api/public/branding/"+kind, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("default %s code=%d", kind, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
			t.Fatalf("default %s content-type=%q", kind, ct)
		}
		if len(body) == 0 {
			t.Fatalf("default %s empty", kind)
		}
	}

	for _, name := range []string{"/favicon.ico", "/favicon.png", "/apple-touch-icon.png"} {
		resp, body := anon.do("GET", env.public.URL+name, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s code=%d", name, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
			t.Fatalf("%s served the SPA HTML (%s)", name, ct)
		}
		if len(body) == 0 {
			t.Fatalf("%s empty", name)
		}
	}
}

// A custom upload replaces the bundled default and is served back unchanged.
func TestBrandingCustomUploadReplacesDefault(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	custom := pngBytes(t)
	if code := uploadBranding(t, admin, env.admin.URL+"/api/admin/branding/logo", "logo.png", custom); code != http.StatusOK {
		t.Fatalf("upload status=%d", code)
	}

	resp, body := admin.do("GET", env.admin.URL+"/api/admin/branding/logo", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("custom logo code=%d", resp.StatusCode)
	}
	if !bytes.Equal(body, custom) {
		t.Fatalf("served logo differs from upload (%d vs %d bytes)", len(body), len(custom))
	}

	// Deleting falls back to the bundled default again.
	if code := admin.json("DELETE", env.admin.URL+"/api/admin/branding/logo", nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status=%d", code)
	}
	resp, body = admin.do("GET", env.admin.URL+"/api/admin/branding/logo", nil)
	if resp.StatusCode != http.StatusOK || bytes.Equal(body, custom) {
		t.Fatalf("after delete code=%d, still custom=%v", resp.StatusCode, bytes.Equal(body, custom))
	}
}
