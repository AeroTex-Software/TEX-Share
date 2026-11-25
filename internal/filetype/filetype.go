// Package filetype centralises upload type policy and preview decisions.
package filetype

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
)

// DangerousMIME lists content types that must never be treated as ordinary
// downloads when detected, regardless of extension.
var DangerousMIME = map[string]bool{
	"application/x-msdownload":                  true,
	"application/x-dosexec":                     true,
	"application/x-msi":                         true,
	"application/x-sh":                          true,
	"application/x-bat":                         true,
	"application/x-executable":                  true,
	"application/vnd.microsoft.portable-executable": true,
	"application/x-mach-binary":                 true,
	"application/x-elf":                         true,
	"text/x-shellscript":                        true,
	"application/java-archive":                  true,
}

// Detect returns a best-effort MIME type using magic bytes first and the
// extension as a hint.
func Detect(filename string, header []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	detected := http.DetectContentType(header)
	if detected == "application/octet-stream" || detected == "text/plain; charset=utf-8" {
		if byExt := mimeByExt(ext); byExt != "" {
			return byExt
		}
	}
	return detected
}

func mimeByExt(ext string) string {
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".svg":
		return "image/svg+xml"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".txt", ".md", ".markdown", ".log":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".go", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".py", ".rs", ".java", ".c", ".h", ".cpp", ".hpp", ".cs", ".php", ".rb", ".sh", ".bash", ".zsh", ".sql", ".html", ".htm", ".css", ".scss", ".xml", ".toml", ".ini", ".conf", ".env":
		return "text/plain; charset=utf-8"
	case ".zip":
		return "application/zip"
	}
	return ""
}

// DangerousByMagic inspects the first bytes for executables and scripts.
func DangerousByMagic(header []byte) (string, bool) {
	switch {
	case len(header) >= 2 && header[0] == 'M' && header[1] == 'Z':
		return "Windows executable (MZ)", true
	case len(header) >= 4 && bytes.Equal(header[:4], []byte{0x7f, 'E', 'L', 'F'}):
		return "ELF executable", true
	case len(header) >= 4 && (bytes.Equal(header[:4], []byte{0xfe, 0xed, 0xfa, 0xce}) || bytes.Equal(header[:4], []byte{0xce, 0xfa, 0xed, 0xfe}) || bytes.Equal(header[:4], []byte{0xcf, 0xfa, 0xed, 0xfe})):
		return "Mach-O executable", true
	case len(header) >= 4 && bytes.Equal(header[:4], []byte{0xca, 0xfe, 0xba, 0xbe}):
		return "Java class file", true
	case len(header) >= 2 && header[0] == '#' && header[1] == '!':
		line := string(header)
		if i := strings.IndexByte(line, '\n'); i > 0 {
			line = line[:i]
		}
		if strings.Contains(line, "sh") || strings.Contains(line, "bash") || strings.Contains(line, "python") || strings.Contains(line, "perl") || strings.Contains(line, "ruby") {
			return "script with shebang", true
		}
	}
	return "", false
}

// CheckUpload applies extension, MIME and magic-byte policy. It returns a
// human-readable reason when blocked.
func CheckUpload(filename, declaredMIME string, header []byte, blockedExts, blockedMIMEs []string) (blocked bool, reason string) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	for _, b := range blockedExts {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(b), "."), ext) {
			return true, "file extension is blocked by policy"
		}
	}
	detected := Detect(filename, header)
	for _, m := range blockedMIMEs {
		if strings.EqualFold(strings.TrimSpace(m), declaredMIME) || strings.EqualFold(strings.TrimSpace(m), detected) {
			return true, "file type is blocked by policy"
		}
	}
	if DangerousMIME[strings.ToLower(strings.Split(declaredMIME, ";")[0])] {
		return true, "file type is blocked by policy"
	}
	if DangerousMIME[strings.ToLower(strings.Split(detected, ";")[0])] {
		return true, "file type is blocked by policy"
	}
	if why, bad := DangerousByMagic(header); bad {
		return true, why + " blocked by policy"
	}
	return false, ""
}

// PreviewKind classifies a file for the public preview UI.
func PreviewKind(filename, mime string) string {
	m := strings.ToLower(strings.Split(mime, ";")[0])
	ext := strings.ToLower(filepath.Ext(filename))
	switch {
	case m == "image/png" || m == "image/jpeg" || m == "image/gif" || m == "image/webp" || m == "image/avif" || m == "image/bmp":
		return "image"
	case m == "video/mp4" || m == "video/webm":
		return "video"
	case m == "audio/mpeg" || m == "audio/ogg" || m == "audio/wav":
		return "audio"
	case m == "application/pdf" || ext == ".pdf":
		return "pdf"
	case strings.HasPrefix(m, "text/") || m == "application/json" || m == "application/xml" || isTextExt(ext):
		return "text"
	}
	return "none"
}

func isTextExt(ext string) bool {
	switch ext {
	case ".txt", ".md", ".markdown", ".log", ".json", ".yaml", ".yml", ".go", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx",
		".py", ".rs", ".java", ".c", ".h", ".cpp", ".hpp", ".cs", ".php", ".rb", ".sh", ".bash", ".zsh", ".sql", ".html", ".htm",
		".css", ".scss", ".xml", ".toml", ".ini", ".conf", ".env", ".csv", ".tsv":
		return true
	}
	return false
}

// InlineAllowed is the strict allowlist for inline Content-Disposition. SVG and
// HTML are intentionally excluded.
func InlineAllowed(mime string) bool {
	m := strings.ToLower(strings.Split(mime, ";")[0])
	switch m {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/bmp",
		"video/mp4", "video/webm", "audio/mpeg", "audio/ogg", "audio/wav",
		"application/pdf", "text/plain", "application/json":
		return true
	}
	return false
}

// IsRasterImage reports whether the type is safe to render in an <img> tag.
func IsRasterImage(mime string) bool {
	m := strings.ToLower(strings.Split(mime, ";")[0])
	switch m {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/bmp":
		return true
	}
	return false
}

// LanguageForExt maps an extension to a highlight.js language hint.
func LanguageForExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".sh", ".bash", ".zsh":
		return "bash"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".rb":
		return "ruby"
	case ".md", ".markdown":
		return "markdown"
	case ".xml":
		return "xml"
	}
	return "plaintext"
}
