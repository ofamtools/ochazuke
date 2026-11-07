package ochazuke

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	pickerPreviewBytes   = 64 * 1024
	pickerPreviewEntries = 128
)

// readPickerPreview is the find source's preview of path (path_find.go):
// what it is, then a file's first pickerPreviewBytes or a folder's first
// pickerPreviewEntries names, or why it cannot be read.
func readPickerPreview(path string) (metadata, text, diagnostic string) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "symlink", "", err.Error()
		}
		return "symlink", "Target: " + target, ""
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return info.Mode().String(), "No text preview for this file type", ""
	}
	file, err := os.Open(path)
	if err != nil {
		return "", "", err.Error()
	}
	defer file.Close()
	if info.IsDir() {
		entries, err := file.ReadDir(pickerPreviewEntries + 1)
		if err != nil && err != io.EOF {
			return "folder", "", err.Error()
		}
		more := len(entries) > pickerPreviewEntries
		if more {
			entries = entries[:pickerPreviewEntries]
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return entries[i].Name() < entries[j].Name()
		})
		lines := make([]string, 0, len(entries)+1)
		for _, entry := range entries {
			name := strings.ReplaceAll(entry.Name(), "\n", "\\n")
			if entry.IsDir() {
				name += "/"
			}
			lines = append(lines, name)
		}
		if more {
			lines = append(lines, "… more entries")
		} else if len(lines) == 0 {
			lines = append(lines, "Empty folder")
		}
		return "folder", strings.Join(lines, "\n"), ""
	}
	metadata = fmt.Sprintf("file · %d bytes", info.Size())
	data, err := io.ReadAll(io.LimitReader(file, pickerPreviewBytes+1))
	if err != nil {
		return metadata, "", err.Error()
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return metadata, "Binary file — no text preview", ""
	}
	truncated := len(data) > pickerPreviewBytes
	if truncated {
		data = data[:pickerPreviewBytes]
	}
	text = strings.ToValidUTF8(string(data), "�")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	if truncated {
		text += "\n… preview truncated"
	} else if text == "" {
		text = "Empty file"
	}
	return metadata, text, ""
}
