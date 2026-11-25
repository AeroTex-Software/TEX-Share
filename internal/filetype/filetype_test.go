package filetype

import (
	"bytes"
	"testing"
)

func TestDetectByMagicAndExtension(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if got := Detect("image.png", png); got != "image/png" {
		t.Errorf("Detect(png) = %q", got)
	}
	// A text file with a known extension falls back to the extension mapping.
	if got := Detect("readme.md", []byte("hello world")); got != "text/plain; charset=utf-8" {
		t.Errorf("Detect(md) = %q", got)
	}
	if got := Detect("data.pdf", []byte("%PDF-1.7")); got != "application/pdf" {
		t.Errorf("Detect(pdf) = %q", got)
	}
}

func TestDangerousByMagic(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"windows exe", []byte{'M', 'Z', 0x90, 0x00}, true},
		{"elf", []byte{0x7f, 'E', 'L', 'F'}, true},
		{"mach-o", []byte{0xfe, 0xed, 0xfa, 0xce}, true},
		{"java class", []byte{0xca, 0xfe, 0xba, 0xbe}, true},
		{"bash shebang", []byte("#!/bin/bash\nrm -rf /"), true},
		{"python shebang", []byte("#!/usr/bin/env python3\nprint(1)"), true},
		{"plain text", []byte("just a normal file"), false},
		{"pdf", []byte("%PDF-1.7"), false},
	}
	for _, c := range cases {
		if _, bad := DangerousByMagic(c.data); bad != c.want {
			t.Errorf("%s: DangerousByMagic = %v, want %v", c.name, bad, c.want)
		}
	}
}

func TestCheckUploadPolicy(t *testing.T) {
	// Blocked extension.
	if blocked, _ := CheckUpload("virus.exe", "application/octet-stream", nil, []string{"exe"}, nil); !blocked {
		t.Error("blocked extension was allowed")
	}
	// Blocked declared MIME.
	if blocked, _ := CheckUpload("a.bin", "application/x-msdownload", nil, nil, []string{"application/x-msdownload"}); !blocked {
		t.Error("blocked MIME was allowed")
	}
	// Executable detected by magic even with an innocent name.
	if blocked, _ := CheckUpload("photo.png", "image/png", []byte{'M', 'Z', 0x90}, nil, nil); !blocked {
		t.Error("MZ payload was allowed")
	}
	// A normal file passes.
	if blocked, reason := CheckUpload("notes.txt", "text/plain", []byte("hello"), nil, nil); blocked {
		t.Errorf("normal file blocked: %s", reason)
	}
}

func TestPreviewAndInlinePolicy(t *testing.T) {
	if PreviewKind("a.png", "image/png") != "image" {
		t.Error("png preview kind wrong")
	}
	if PreviewKind("a.mp4", "video/mp4") != "video" {
		t.Error("mp4 preview kind wrong")
	}
	if PreviewKind("a.pdf", "application/pdf") != "pdf" {
		t.Error("pdf preview kind wrong")
	}
	if PreviewKind("a.bin", "application/octet-stream") != "none" {
		t.Error("binary preview kind should be none")
	}
	// SVG and HTML are never inlined.
	if InlineAllowed("image/svg+xml") || InlineAllowed("text/html") {
		t.Error("dangerous type allowed inline")
	}
	if !InlineAllowed("image/png") || !InlineAllowed("application/pdf") {
		t.Error("safe type rejected for inline")
	}
}

func TestLanguageForExt(t *testing.T) {
	if LanguageForExt("main.go") != "go" || LanguageForExt("x.ts") != "typescript" || LanguageForExt("y.unknown") != "plaintext" {
		t.Error("language mapping incorrect")
	}
	_ = bytes.MinRead
}
