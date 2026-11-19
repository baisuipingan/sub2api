package service

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

var officeFileMIMEs = map[string]string{
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".pdf":  "application/pdf", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp",
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".json": "application/json",
}

func officeSafeName(name string) bool {
	if name == "" || len(name) > 240 || !utf8.ValidString(name) || path.Base(name) != name || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

// Validate by extension AND actual bytes. Zip metadata is bounded before any
// decompression; macros, encryption, links and path escapes are never accepted.
func validateOfficeFile(name string, data []byte) (string, string, error) {
	if !officeSafeName(name) {
		return "", "", ErrOfficeInvalid
	}
	if len(data) == 0 || int64(len(data)) > OfficeResourceMaxBytes {
		return "", "", ErrOfficeResourceTooLarge
	}
	ext := strings.ToLower(path.Ext(name))
	mime, ok := officeFileMIMEs[ext]
	if !ok {
		return "", "", ErrOfficeResourceType
	}
	kind := "unsupported"
	switch ext {
	case ".pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return "", "", ErrOfficeResourceType
		}
		kind = "pdf"
	case ".png", ".jpg", ".jpeg", ".webp":
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || galleryImageMIME(format) != mime {
			return "", "", ErrOfficeResourceType
		}
		if _, ok := studioImagePixels(cfg.Width, cfg.Height); !ok {
			return "", "", ErrOfficeResourceType
		}
		kind = "image"
	case ".docx", ".xlsx", ".pptx":
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(z.File) > 10000 {
			return "", "", ErrOfficeResourceType
		}
		required := map[string]string{".docx": "word/document.xml", ".xlsx": "xl/workbook.xml", ".pptx": "ppt/presentation.xml"}[ext]
		names := map[string]bool{}
		var unpacked uint64
		var contentTypes *zip.File
		for _, f := range z.File {
			name := strings.TrimSuffix(f.Name, "/")
			if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(name) != name || strings.HasPrefix(name, "../") || f.Mode()&(os.ModeSymlink|os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 || f.Flags&1 != 0 || names[f.Name] {
				return "", "", ErrOfficeResourceType
			}
			if strings.Contains(strings.ToLower(f.Name), "vbaproject") {
				return "", "", ErrOfficeResourceType
			}
			unpacked += f.UncompressedSize64
			if unpacked > 128<<20 {
				return "", "", ErrOfficeResourceType
			}
			names[f.Name] = true
			if f.Name == "[Content_Types].xml" {
				contentTypes = f
			}
		}
		if !names[required] || contentTypes == nil || contentTypes.UncompressedSize64 > 1<<20 {
			return "", "", ErrOfficeResourceType
		}
		reader, err := contentTypes.Open()
		if err != nil {
			return "", "", ErrOfficeResourceType
		}
		metadata, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
		_ = reader.Close()
		if err != nil || len(metadata) > 1<<20 {
			return "", "", ErrOfficeResourceType
		}
		lower := bytes.ToLower(metadata)
		for _, marker := range []string{"macroenabled", "vbaproject", "macrosheet"} {
			if bytes.Contains(lower, []byte(marker)) {
				return "", "", ErrOfficeResourceType
			}
		}
	case ".txt", ".md", ".csv", ".json":
		if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
			return "", "", ErrOfficeResourceType
		}
		if ext == ".json" && !json.Valid(data) {
			return "", "", ErrOfficeResourceType
		}
		kind = "text"
		if ext == ".md" {
			kind = "markdown"
		}
	}
	return mime, kind, nil
}

func officeWorkspacePath(value string) bool {
	if len(value) > 768 || !utf8.ValidString(value) || strings.Contains(value, "\\") || path.Clean(value) != value || strings.HasPrefix(value, "/") {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return false
		}
	}
	parts := strings.Split(value, "/")
	return len(parts) >= 2 && len(parts) <= 12 && (parts[0] == "uploads" || parts[0] == "output" || parts[0] == "scratch") && parts[len(parts)-1] != ".." && !strings.Contains(value, "/../")
}

func validateOfficeWorkspace(files []OfficeWorkspaceFile) (map[string][]byte, error) {
	if len(files) > OfficeWorkspaceMaxFiles {
		return nil, ErrOfficeResourceTooLarge
	}
	seen := map[string][]byte{}
	var total int64
	for _, f := range files {
		if !officeWorkspacePath(f.Path) || f.Size < 0 || f.Size > OfficeResourceMaxBytes || len(f.ContentBase64) > ((int(OfficeResourceMaxBytes)+2)/3)*4 {
			return nil, ErrOfficeInvalid
		}
		for name := range seen {
			if name == f.Path || strings.HasPrefix(name, f.Path+"/") || strings.HasPrefix(f.Path, name+"/") {
				return nil, ErrOfficeInvalid
			}
		}
		data, err := base64.StdEncoding.Strict().DecodeString(f.ContentBase64)
		if err != nil || int64(len(data)) != f.Size || officeResourceHash(data) != f.SHA256 {
			return nil, ErrOfficeInvalid
		}
		total += int64(len(data))
		if total > OfficeResourceMaxBytes {
			return nil, ErrOfficeResourceTooLarge
		}
		seen[f.Path] = data
	}
	return seen, nil
}
