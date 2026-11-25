package storage

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	bad := []string{
		"../escape",
		"..",
		"a/../../b",
		"../../etc/passwd",
	}
	for _, p := range bad {
		if _, err := m.resolve(p); err == nil {
			t.Errorf("resolve(%q) allowed a traversal", p)
		}
	}
	// Any accepted path must remain inside the storage root, regardless of how
	// the host OS treats a leading slash.
	for _, p := range []string{"/abs/path", "files/ok.bin", "./nested/file"} {
		full, err := m.resolve(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, full)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("resolve(%q) escaped root: %q", p, full)
		}
	}
}

func TestWriteDirectAndChunks(t *testing.T) {
	m := New(t.TempDir())
	if err := m.EnsureUpload("up1"); err != nil {
		t.Fatalf("EnsureUpload: %v", err)
	}
	payload := []byte("hello chunked world")
	rel, _, digest, size, err := m.WriteDirect("up1", bytes.NewReader(payload), 1<<20)
	if err != nil {
		t.Fatalf("WriteDirect: %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size = %d", size)
	}
	f, err := m.Open(rel)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	buf := make([]byte, len(payload))
	if _, err := f.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("content mismatch: %q", buf)
	}
	if digest == "" {
		t.Fatal("empty digest")
	}

	// Oversize direct write is rejected.
	if _, _, _, _, err := m.WriteDirect("up1", bytes.NewReader(make([]byte, 100)), 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestChunkAssembly(t *testing.T) {
	m := New(t.TempDir())
	if err := m.EnsureUpload("up2"); err != nil {
		t.Fatal(err)
	}
	part0 := []byte("aaaa")
	part1 := []byte("bbbb")
	part2 := []byte("cc")
	if _, err := m.WriteChunk("up2", 0, bytes.NewReader(part0), 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WriteChunk("up2", 1, bytes.NewReader(part1), 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WriteChunk("up2", 2, bytes.NewReader(part2), 1<<20); err != nil {
		t.Fatal(err)
	}
	info, err := m.ReceivedChunks("up2")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Indices) != 3 || info.Bytes != 10 {
		t.Fatalf("ReceivedChunks = %+v", info)
	}
	rel, _, _, err := m.AssembleChunks("up2", 3, 10)
	if err != nil {
		t.Fatalf("AssembleChunks: %v", err)
	}
	f, err := m.Open(rel)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 10)
	if _, err := f.Read(buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "aaaabbbbcc" {
		t.Fatalf("assembled = %q", buf)
	}

	// Missing chunks must be reported as incomplete.
	if _, _, _, err := m.AssembleChunks("up2", 4, 10); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("expected ErrIncomplete, got %v", err)
	}
}
