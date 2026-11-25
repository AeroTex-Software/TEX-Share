// Package storage owns the on-disk layout for uploads, share blobs, avatars and
// quarantine. All paths are server-generated; user input never becomes a path.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	ErrTooLarge   = errors.New("file too large")
	ErrNotFound   = errors.New("object not found")
	ErrBadChunk   = errors.New("invalid chunk")
	ErrIncomplete = errors.New("upload incomplete")
)

// Manager implements the local filesystem store.
type Manager struct {
	root string
	mu   sync.Mutex
}

func New(root string) *Manager { return &Manager{root: root} }

func (m *Manager) Root() string { return m.root }

func (m *Manager) resolve(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || strings.Contains(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid storage path")
	}
	full := filepath.Join(m.root, clean)
	relCheck, err := filepath.Rel(m.root, full)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", errors.New("path escapes storage root")
	}
	return full, nil
}

// Open opens a stored file by relative path.
func (m *Manager) Open(rel string) (*os.File, error) {
	full, err := m.resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

// Stat returns file info for a relative path.
func (m *Manager) Stat(rel string) (os.FileInfo, error) {
	full, err := m.resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.Stat(full)
}

// Remove deletes a relative file or directory.
func (m *Manager) Remove(rel string) error {
	full, err := m.resolve(rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(full)
}

// RemoveShare deletes the directory containing a share's files.
func (m *Manager) RemoveShare(shareID string) error {
	return m.Remove(filepath.Join("files", safeID(shareID)))
}

// UploadDir returns the staging directory for an upload.
func (m *Manager) UploadDir(uploadID string) string {
	return filepath.Join("uploads", safeID(uploadID))
}

// EnsureUpload creates the staging directory.
func (m *Manager) EnsureUpload(uploadID string) error {
	full, err := m.resolve(m.UploadDir(uploadID))
	if err != nil {
		return err
	}
	return os.MkdirAll(full, 0o750)
}

// WriteDirect streams a direct upload into its staging directory and returns
// the temporary path, detected header bytes and SHA-256 digest.
func (m *Manager) WriteDirect(uploadID string, r io.Reader, limit int64) (relPath string, header []byte, digest string, size int64, err error) {
	if err := m.EnsureUpload(uploadID); err != nil {
		return "", nil, "", 0, err
	}
	relPath = filepath.Join(m.UploadDir(uploadID), "blob.part")
	full, err := m.resolve(relPath)
	if err != nil {
		return "", nil, "", 0, err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return "", nil, "", 0, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	hasher := sha256.New()
	limited := io.LimitReader(r, limit+1)
	header = make([]byte, 512)
	n, readErr := io.ReadFull(limited, header)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return "", nil, "", 0, readErr
	}
	header = header[:n]
	if int64(n) > limit {
		return "", nil, "", 0, ErrTooLarge
	}
	if _, err = f.Write(header); err != nil {
		return "", nil, "", 0, err
	}
	hasher.Write(header)
	size = int64(n)
	written, err := io.Copy(io.MultiWriter(f, hasher), limited)
	if err != nil {
		return "", nil, "", 0, err
	}
	size += written
	if size > limit {
		return "", nil, "", 0, ErrTooLarge
	}
	if err := f.Sync(); err != nil {
		return "", nil, "", 0, err
	}
	return relPath, header, hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// WriteChunk stores one chunk atomically. index is zero based.
func (m *Manager) WriteChunk(uploadID string, index int, r io.Reader, limit int64) (int64, error) {
	if index < 0 || index > 1_000_000 {
		return 0, ErrBadChunk
	}
	if err := m.EnsureUpload(uploadID); err != nil {
		return 0, err
	}
	rel := filepath.Join(m.UploadDir(uploadID), fmt.Sprintf("chunk-%08d.part", index))
	full, err := m.resolve(rel)
	if err != nil {
		return 0, err
	}
	tmp := full + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	limited := io.LimitReader(r, limit+1)
	n, copyErr := io.Copy(f, limited)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return 0, copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return 0, closeErr
	}
	if n > limit {
		os.Remove(tmp)
		return 0, ErrTooLarge
	}
	if err := os.Rename(tmp, full); err != nil {
		return 0, err
	}
	return n, nil
}

// ChunkInfo describes received chunks for resume support.
type ChunkInfo struct {
	Indices []int  `json:"indices"`
	Bytes   int64  `json:"bytes"`
}

// ReceivedChunks scans the staging directory.
func (m *Manager) ReceivedChunks(uploadID string) (ChunkInfo, error) {
	full, err := m.resolve(m.UploadDir(uploadID))
	if err != nil {
		return ChunkInfo{}, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChunkInfo{}, ErrNotFound
		}
		return ChunkInfo{}, err
	}
	info := ChunkInfo{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "chunk-") || !strings.HasSuffix(name, ".part") {
			continue
		}
		num := strings.TrimSuffix(strings.TrimPrefix(name, "chunk-"), ".part")
		idx, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		info.Indices = append(info.Indices, idx)
		info.Bytes += fi.Size()
	}
	sort.Ints(info.Indices)
	return info, nil
}

// AssembleChunks concatenates all chunks into blob.part. It validates count,
// total size and removes chunk files afterwards.
func (m *Manager) AssembleChunks(uploadID string, totalChunks int, totalSize int64) (relPath string, header []byte, digest string, err error) {
	if totalChunks <= 0 || totalChunks > 1_000_000 {
		return "", nil, "", ErrIncomplete
	}
	dir, err := m.resolve(m.UploadDir(uploadID))
	if err != nil {
		return "", nil, "", err
	}
	relPath = filepath.Join(m.UploadDir(uploadID), "blob.part")
	full, err := m.resolve(relPath)
	if err != nil {
		return "", nil, "", err
	}
	out, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return "", nil, "", err
	}
	hasher := sha256.New()
	var written int64
	header = make([]byte, 0, 512)
	buf := make([]byte, 512)
	for i := 0; i < totalChunks; i++ {
		chunkPath := filepath.Join(dir, fmt.Sprintf("chunk-%08d.part", i))
		cf, err := os.Open(chunkPath)
		if err != nil {
			out.Close()
			return "", nil, "", fmt.Errorf("chunk %d missing: %w", i, ErrIncomplete)
		}
		if i == 0 {
			n, _ := io.ReadFull(cf, buf)
			header = append(header, buf[:n]...)
			if _, err := cf.Seek(0, io.SeekStart); err != nil {
				cf.Close()
				out.Close()
				return "", nil, "", err
			}
		}
		n, err := io.Copy(io.MultiWriter(out, hasher), cf)
		cf.Close()
		if err != nil {
			out.Close()
			return "", nil, "", err
		}
		written += n
	}
	if written != totalSize {
		out.Close()
		return "", nil, "", fmt.Errorf("%w: got %d bytes, expected %d", ErrIncomplete, written, totalSize)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return "", nil, "", err
	}
	if err := out.Close(); err != nil {
		return "", nil, "", err
	}
	for i := 0; i < totalChunks; i++ {
		os.Remove(filepath.Join(dir, fmt.Sprintf("chunk-%08d.part", i)))
	}
	return relPath, header, hex.EncodeToString(hasher.Sum(nil)), nil
}

// Attach moves a completed upload blob into a share directory and returns the
// final relative path and size.
func (m *Manager) Attach(uploadID, shareID, fileID string) (string, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	srcRel := filepath.Join(m.UploadDir(uploadID), "blob.part")
	src, err := m.resolve(srcRel)
	if err != nil {
		return "", 0, err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return "", 0, ErrNotFound
	}
	destRel := filepath.Join("files", safeID(shareID), safeID(fileID)+".bin")
	dest, err := m.resolve(destRel)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", 0, err
	}
	if err := os.Rename(src, dest); err != nil {
		// Cross-device fallback: stream the file.
		in, err := os.Open(src)
		if err != nil {
			return "", 0, err
		}
		defer in.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			return "", 0, err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return "", 0, err
		}
		if err := out.Close(); err != nil {
			return "", 0, err
		}
		os.Remove(src)
	}
	os.RemoveAll(filepath.Join(m.root, m.UploadDir(uploadID)))
	return filepath.ToSlash(destRel), fi.Size(), nil
}

// Move relocates a stored file. It is used to compensate failed transactions.
func (m *Manager) Move(srcRel, destRel string) error {
	src, err := m.resolve(srcRel)
	if err != nil {
		return err
	}
	dest, err := m.resolve(destRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	if err := os.Rename(src, dest); err != nil {
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		os.Remove(src)
	}
	return nil
}

// SaveAvatar stores a profile image after validating its size and type.
func (m *Manager) SaveAvatar(userID int64, data []byte, ext string) (string, error) {
	rel := filepath.Join("branding", fmt.Sprintf("avatar-%d%s", userID, ext))
	full, err := m.resolve(rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o640); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// SaveBranding stores a logo or favicon.
func (m *Manager) SaveBranding(name string, data []byte) (string, error) {
	rel := filepath.Join("branding", safeID(name))
	full, err := m.resolve(rel)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o640); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// MoveToQuarantine moves a file out of the active share tree.
func (m *Manager) MoveToQuarantine(relPath, shareID, fileID string) (string, error) {
	src, err := m.resolve(relPath)
	if err != nil {
		return "", err
	}
	destRel := filepath.Join("quarantine", safeID(shareID), safeID(fileID)+".bin")
	dest, err := m.resolve(destRel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", err
	}
	if err := os.Rename(src, dest); err != nil {
		return "", err
	}
	return filepath.ToSlash(destRel), nil
}

// TempFile creates a temporary file inside the data directory.
func (m *Manager) TempFile(pattern string) (*os.File, error) {
	dir, err := m.resolve("tmp")
	if err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, pattern)
}

func safeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "invalid"
	}
	return b.String()
}
