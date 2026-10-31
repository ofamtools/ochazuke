package diffview

import (
	"strings"
	"testing"
)

func TestDiffParse(t *testing.T) {
	files, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if len(files) != 1 || f.OldPath != "demo.go" || f.NewPath != "demo.go" || f.Added != 5 || f.Removed != 4 || len(f.Hunks) != 3 {
		t.Fatalf("file=%+v", f)
	}
	h := f.Hunks[0]
	if h.Lines[1].Kind != Delete || h.Lines[1].Old != 2 || h.Lines[1].New != 0 || h.Lines[3].Kind != Add || h.Lines[3].New != 2 || h.Lines[3].Old != 0 {
		t.Fatalf("line numbers=%+v", h.Lines)
	}
}
func TestDiffParseMetadata(t *testing.T) {
	text := `diff --git a/old name.go b/new name.go
similarity index 100%
rename from old name.go
rename to new name.go
diff --git "a/quoted\tname" "b/quoted\tname"
deleted file mode 100644
--- "a/quoted\tname"
+++ /dev/null
@@ -1 +0,0 @@
-deleted
\ No newline at end of file
diff --git a/image.bin b/image.bin
Binary files a/image.bin and b/image.bin differ
diff --git a/fresh b/fresh
new file mode 100644
--- /dev/null
+++ b/fresh
@@ -0,0 +1 @@
+new
`
	files, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatal(len(files))
	}
	if files[0].Status != "R" || files[0].OldPath != "old name.go" || files[0].NewPath != "new name.go" {
		t.Fatal(files[0])
	}
	if files[1].Status != "D" || files[1].NewPath != "" || files[1].OldPath != "quoted\tname" || !files[1].Hunks[0].Lines[0].NoNewline {
		t.Fatal(files[1])
	}
	if !files[2].Binary || files[2].OldSize != -1 {
		t.Fatal(files[2])
	}
	if files[3].Status != "A" || files[3].OldPath != "" || files[3].NewPath != "fresh" {
		t.Fatal(files[3])
	}
}
func TestDiffParsePlainAndLong(t *testing.T) {
	text := "--- a/old\t2026-01-01\n+++ b/new\t2026-01-01\n@@ -1 +1 @@\n-old\n+" + strings.Repeat("x", 100000) + "\n"
	files, err := Parse(text + "--- a/deleted\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n--- /dev/null\n+++ b/added\n@@ -0,0 +1 @@\n+y\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || len(files[0].Hunks[0].Lines[1].Text) != 100000 || files[0].NewPath != "new" {
		t.Fatalf("files=%d", len(files))
	}
}
func TestDiffParseRejectsMalformed(t *testing.T) {
	for _, text := range []string{
		"@@ -1 +1 @@\n-x\n+y", "--- a/x\n+++ b/x\n@@ bad @@", "--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n one\n", "--- a/x\n+++ b/x\n@@ -0,0 +1 @@\n-x", "diff --cc merge", "--- a/x\n+++ b/x\n@@ -999999999999999999999 +1 @@",
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}
