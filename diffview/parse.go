package diffview

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// Parse reads ordinary unified patches, including multi-file git diffs,
// renames, copies, additions, deletions, binary notices and missing newlines.
// It does not execute diff drivers or infer binary sizes (git's patch text
// omits them); a host with metadata fills Sizes and Truncated itself.
// Combined merge diffs and malformed/incomplete hunks return an error.
func Parse(text string) ([]File, error) {
	var files []File
	var file *File
	var hunk *Hunk
	old, newLine, oldLeft, newLeft := 0, 0, 0, 0
	finish := func() error {
		if hunk != nil && (oldLeft != 0 || newLeft != 0) {
			return fmt.Errorf("incomplete hunk: %d old and %d new lines missing", oldLeft, newLeft)
		}
		hunk = nil
		return nil
	}
	start := func() {
		files = append(files, File{Status: "M", OldSize: -1, NewSize: -1})
		file = &files[len(files)-1]
	}
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		fail := func(s string) ([]File, error) { return nil, fmt.Errorf("diff line %d: %s", i+1, s) }
		if strings.HasPrefix(line, "diff --cc ") || strings.HasPrefix(line, "diff --combined ") || strings.HasPrefix(line, "@@@") {
			return fail("combined diffs are unsupported")
		}
		if hunk != nil {
			if line == `\ No newline at end of file` {
				if len(hunk.Lines) == 0 {
					return fail("newline marker without a line")
				}
				hunk.Lines[len(hunk.Lines)-1].NoNewline = true
				continue
			}
			if oldLeft > 0 || newLeft > 0 {
				if len(line) == 0 {
					return fail("missing diff line sign")
				}
				l := Line{Text: line[1:]}
				switch line[0] {
				case ' ':
					l.Kind = Context
					l.Old = old
					l.New = newLine
					old++
					newLine++
					oldLeft--
					newLeft--
				case '-':
					l.Kind = Delete
					l.Old = old
					old++
					oldLeft--
					file.Removed++
				case '+':
					l.Kind = Add
					l.New = newLine
					newLine++
					newLeft--
					file.Added++
				default:
					return fail("unexpected line inside hunk")
				}
				if oldLeft < 0 || newLeft < 0 {
					return fail("hunk exceeds its declared range")
				}
				hunk.Lines = append(hunk.Lines, l)
				continue
			}
			if err := finish(); err != nil {
				return fail(err.Error())
			}
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			start()
			// With core.quotePath=false paths containing spaces are unquoted.
			// Prefer ---/+++ or rename metadata when present; for binary/mode-only
			// patches the a/… b/… boundary is the best information git supplies.
			paths := strings.TrimPrefix(line, "diff --git ")
			if strings.HasPrefix(paths, `"`) {
				if end := quotedEnd(paths); end > 0 {
					file.OldPath = patchPath(paths[:end])
					file.NewPath = patchPath(strings.TrimSpace(paths[end:]))
				}
			} else if a, b, ok := strings.Cut(paths, " b/"); ok {
				file.OldPath = patchPath(a)
				file.NewPath = b
			}
		case strings.HasPrefix(line, "--- "):
			if file == nil || len(file.Hunks) > 0 || file != nil && file.Binary {
				start()
			}
			file.OldPath = patchPath(strings.TrimPrefix(line, "--- "))
			if file.OldPath == "" {
				file.Status = "A"
			}
		case strings.HasPrefix(line, "+++ "):
			if file == nil {
				return fail("new path without old path")
			}
			file.NewPath = patchPath(strings.TrimPrefix(line, "+++ "))
			if file.NewPath == "" {
				file.Status = "D"
			}
		case strings.HasPrefix(line, "@@"):
			if file == nil {
				return fail("hunk without a file")
			}
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				return fail("invalid hunk header")
			}
			nums := [4]int{0, 1, 0, 1}
			for j := range 4 {
				if m[j+1] != "" {
					n, e := strconv.Atoi(m[j+1])
					if e != nil {
						return fail("range overflows integer")
					}
					nums[j] = n
				}
			}
			file.Hunks = append(file.Hunks, Hunk{OldStart: nums[0], OldCount: nums[1], NewStart: nums[2], NewCount: nums[3], Header: strings.TrimSpace(m[5])})
			hunk = &file.Hunks[len(file.Hunks)-1]
			old, newLine, oldLeft, newLeft = nums[0], nums[2], nums[1], nums[3]
		case strings.HasPrefix(line, "rename from "), strings.HasPrefix(line, "copy from "):
			if file == nil {
				return fail("path metadata without file")
			}
			_, p, _ := strings.Cut(line, " from ")
			file.OldPath = unquote(p)
			if strings.HasPrefix(line, "rename") {
				file.Status = "R"
			} else {
				file.Status = "C"
			}
		case strings.HasPrefix(line, "rename to "), strings.HasPrefix(line, "copy to "):
			if file == nil {
				return fail("path metadata without file")
			}
			_, p, _ := strings.Cut(line, " to ")
			file.NewPath = unquote(p)
		case strings.HasPrefix(line, "new file mode "):
			if file != nil {
				file.Status = "A"
				file.OldPath = ""
				file.OldSize = 0
			}
		case strings.HasPrefix(line, "deleted file mode "):
			if file != nil {
				file.Status = "D"
				file.NewPath = ""
				file.NewSize = 0
			}
		case strings.HasPrefix(line, "Binary files "), line == "GIT binary patch":
			if file == nil {
				return fail("binary notice without file")
			}
			file.Binary = true
		case strings.HasPrefix(line, "+"), strings.HasPrefix(line, "-"):
			return fail("source line outside a hunk")
		}
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return files, nil
}

func quotedEnd(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i + 1
		}
	}
	return 0
}
func unquote(s string) string {
	if v, e := strconv.Unquote(s); e == nil {
		return v
	}
	return s
}
func patchPath(s string) string {
	if strings.HasPrefix(s, `"`) {
		if end := quotedEnd(s); end > 0 {
			s = unquote(s[:end])
		}
	} else {
		s, _, _ = strings.Cut(s, "\t")
	}
	if s == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(s, "a/") || strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	return s
}
