// Package diffview displays data-backed file diffs without running git or
// reading files. Parse accepts unified diff text; New accepts structured data.
package diffview

// File is a diff of one file. Sizes are bytes (-1 if unknown). Added and
// Removed can include omitted lines when Truncated; zero falls back to the
// counts present in Hunks. Paths and text are displayed as untrusted text.
type File struct {
	OldPath   string `json:"oldPath,omitempty"`
	NewPath   string `json:"newPath,omitempty"`
	Status    string `json:"status,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	OldSize   int64  `json:"oldSize,omitempty"`
	NewSize   int64  `json:"newSize,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Added     int    `json:"added,omitempty"`
	Removed   int    `json:"removed,omitempty"`
	Hunks     []Hunk `json:"hunks,omitempty"`
}

// Hunk is a unified diff range and its lines; Header is the optional text
// following @@. Line numbers are explicit, so hosts can build hunks directly.
type Hunk struct {
	OldStart int    `json:"oldStart"`
	OldCount int    `json:"oldCount"`
	NewStart int    `json:"newStart"`
	NewCount int    `json:"newCount"`
	Header   string `json:"header,omitempty"`
	Lines    []Line `json:"lines"`
}

// Line is one logical source line without its diff sign or trailing newline.
// Old/New are zero on the side that has no line. NoNewline preserves git's
// "No newline at end of file" marker without making it a source line.
type Line struct {
	Kind      Kind   `json:"kind"`
	Text      string `json:"text"`
	Old       int    `json:"old,omitempty"`
	New       int    `json:"new,omitempty"`
	NoNewline bool   `json:"noNewline,omitempty"`
}
