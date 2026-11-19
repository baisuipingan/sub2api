package service

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func officeTestZip(t *testing.T, entries map[string]string, symlink string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for name, value := range entries {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if name == symlink {
			h.SetMode(os.ModeSymlink | 0777)
		}
		f, err := w.CreateHeader(h)
		require.NoError(t, err)
		_, err = f.Write([]byte(value))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return out.Bytes()
}

func TestOfficeResourcesRejectDisguisedOfficeAndArchiveEscapes(t *testing.T) {
	valid := map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": "<document/>"}
	_, _, err := validateOfficeFile("报告.docx", officeTestZip(t, valid, ""))
	require.NoError(t, err)
	for _, extra := range []string{"../escape", "/absolute", "word/../../escape", "word\\evil", "word/vbaProject.bin"} {
		t.Run(extra, func(t *testing.T) {
			entries := map[string]string{}
			for k, v := range valid {
				entries[k] = v
			}
			entries[extra] = "x"
			_, _, err := validateOfficeFile("报告.docx", officeTestZip(t, entries, ""))
			require.ErrorIs(t, err, ErrOfficeResourceType)
		})
	}
	_, _, err = validateOfficeFile("报告.docx", officeTestZip(t, valid, "word/document.xml"))
	require.ErrorIs(t, err, ErrOfficeResourceType)
	_, _, err = validateOfficeFile("报告.docx", []byte("not a zip"))
	require.ErrorIs(t, err, ErrOfficeResourceType)
	_, _, err = validateOfficeFile("报告.docx", officeTestZip(t, map[string]string{"[Content_Types].xml": "macroEnabled", "word/document.xml": "x"}, ""))
	require.ErrorIs(t, err, ErrOfficeResourceType)
	_, _, err = validateOfficeFile("报告.xlsx", officeTestZip(t, valid, ""))
	require.ErrorIs(t, err, ErrOfficeResourceType)
}

func TestOfficeResourcesActualTypeAndWorkspaceBoundaries(t *testing.T) {
	for _, name := range []string{"shell.exe", "page.html", "script.svg", "../escape.txt", "a\\b.txt", "line\n.txt"} {
		_, _, err := validateOfficeFile(name, []byte("hello"))
		require.Error(t, err, name)
	}
	_, _, err := validateOfficeFile("fake.png", []byte("<html>"))
	require.ErrorIs(t, err, ErrOfficeResourceType)
	_, _, err = validateOfficeFile("data.json", []byte("{"))
	require.ErrorIs(t, err, ErrOfficeResourceType)
	_, _, err = validateOfficeFile("notes.txt", []byte{0xff, 0xfe})
	require.ErrorIs(t, err, ErrOfficeResourceType)
	data := []byte("中文 office ✅")
	f := OfficeWorkspaceFile{Path: "output/报告.txt", Size: int64(len(data)), SHA256: officeResourceHash(data), MIME: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString(data)}
	_, err = validateOfficeWorkspace([]OfficeWorkspaceFile{f})
	require.NoError(t, err)
	for _, name := range []string{"../escape", "uploads/../x", "/tmp/x", "output/a//b", "output/a/./b", "skills/x", "scratch/x/", "output/a\\b"} {
		bad := f
		bad.Path = name
		_, err = validateOfficeWorkspace([]OfficeWorkspaceFile{bad})
		require.Error(t, err, name)
	}
	bad := f
	bad.SHA256 = strings.Repeat("0", 64)
	_, err = validateOfficeWorkspace([]OfficeWorkspaceFile{bad})
	require.ErrorIs(t, err, ErrOfficeInvalid)
	_, err = validateOfficeWorkspace([]OfficeWorkspaceFile{f, f})
	require.ErrorIs(t, err, ErrOfficeInvalid)
	parent := f
	parent.Path = "output/报告.txt/a"
	_, err = validateOfficeWorkspace([]OfficeWorkspaceFile{f, parent})
	require.ErrorIs(t, err, ErrOfficeInvalid)
	_, err = validateOfficeWorkspace(make([]OfficeWorkspaceFile, OfficeWorkspaceMaxFiles+1))
	require.ErrorIs(t, err, ErrOfficeResourceTooLarge)
}
