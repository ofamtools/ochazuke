package main

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"
)

// matrixRun runs one o-story command line and returns its stdout.
func matrixRun(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	tool := &Tool{Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return t.TempDir() }, Prog: "o-story"}
	if err := tool.Main(args); err != nil {
		t.Fatalf("o-story %s: %v\n%s", strings.Join(args, " "), err, errOut.String())
	}
	return out.String()
}

// The matrix and coverage commands give the same bytes every run, and
// coverage's total counts every cell of the matrix.
func TestMatrixAndCoverageCommandsAreStable(t *testing.T) {
	for _, args := range [][]string{{"matrix"}, {"matrix", "--json", "--component", "tables"}, {"coverage"}, {"coverage", "--component", "lists"}, {"coverage", "--json"}} {
		a, b := matrixRun(t, args...), matrixRun(t, args...)
		if a != b || a == "" {
			t.Fatalf("o-story %s differs between runs or is empty", strings.Join(args, " "))
		}
	}
	cells := strings.Count(matrixRun(t, "matrix"), "\n")
	total := ""
	for _, line := range strings.Split(matrixRun(t, "coverage"), "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[0] == "total" {
			total = f[1]
		}
	}
	if total == "" || total != strconv.Itoa(cells) {
		t.Fatalf("coverage total %q, matrix has %d cells", total, cells)
	}
	if out := matrixRun(t, "coverage", "--component", "lists"); !strings.Contains(out, "lists: missing cells:") {
		t.Fatalf("coverage --component lists lists no missing cells:\n%s", out[:min(len(out), 400)])
	}
	// The embedded map is the file on disk.
	data, err := os.ReadFile("components.json")
	if err != nil || !bytes.Equal(data, componentsJSON) {
		t.Fatalf("embedded components.json differs from the file: %v", err)
	}
}
