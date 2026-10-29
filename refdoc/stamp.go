// Package refdoc builds the generated reference of an ochazuke
// application: a Model of its settings and default key rows, a catalog
// Fingerprint over it, the Markdown pages Render writes from it (each with
// a header naming the build it came from), and Check, which compares those
// pages with the committed ones. It uses the standard library only: the
// root ochazuke package fills a Model from a runtime and imports this one.
package refdoc

import (
	"bytes"
	"cmp"
	"errors"
	"os/exec"
	"runtime/debug"
	"strings"
)

// Build stamps set with the linker, which win over the build info the go
// command embeds: in a linked worktree that build info names the main
// checkout's HEAD, not the worktree's.
//
//	go build -ldflags "-X github.com/unixsuperhero/ochazuke/refdoc.Commit=$(git rev-parse HEAD) \
//	  -X github.com/unixsuperhero/ochazuke/refdoc.Dirty=true"
//
// Dirty is "true" for a build from a tree with uncommitted changes. Date
// is the commit date (YYYY-MM-DD).
var (
	Commit  string
	Version string
	Date    string
	Dirty   string
)

// Stamp names the build a page came from: the tool, its version, the
// commit (the base it was built or generated on), whether the tree was
// dirty, the commit date and the ochazuke module version it carries. An
// empty Commit is an unknown build, never a guess.
type Stamp struct {
	Tool     string `json:"tool"`
	Version  string `json:"version,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Dirty    bool   `json:"dirty,omitempty"`
	Date     string `json:"date,omitempty"`
	Ochazuke string `json:"ochazuke,omitempty"`
}

// ShortCommit is the commit's first 12 characters with "+dirty" for a
// dirty tree, or "unknown build".
func (s Stamp) ShortCommit() string {
	if s.Commit == "" {
		return "unknown build"
	}
	c := s.Commit
	if len(c) > 12 {
		c = c[:12]
	}
	if s.Dirty {
		c += "+dirty"
	}
	return c
}

// BuildStamp is the running binary's stamp: the linker stamps first, then
// the build info (vcs.revision, vcs.time, vcs.modified, the main module's
// version, the ochazuke dependency's version).
func BuildStamp(tool string) Stamp {
	s := Stamp{Tool: tool}
	if bi, ok := debug.ReadBuildInfo(); ok {
		s = fromBuildInfo(tool, bi)
	}
	return withLinkerStamps(s)
}

// withLinkerStamps lays the -X stamps over s: a set Commit replaces the
// build info's commit, date and dirty mark together.
func withLinkerStamps(s Stamp) Stamp {
	if Commit != "" {
		s.Commit, s.Dirty, s.Date = Commit, Dirty == "true", Date
	}
	if Version != "" {
		s.Version = Version
	}
	return s
}

// fromBuildInfo reads a stamp from the go command's build info.
func fromBuildInfo(tool string, bi *debug.BuildInfo) Stamp {
	s := Stamp{Tool: tool, Ochazuke: ochazukeOf(bi)}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		s.Version = v
	}
	for _, kv := range bi.Settings {
		switch kv.Key {
		case "vcs.revision":
			s.Commit = kv.Value
		case "vcs.time":
			s.Date, _, _ = strings.Cut(kv.Value, "T")
		case "vcs.modified":
			s.Dirty = kv.Value == "true"
		}
	}
	return s
}

// ochazukeOf is the ochazuke module version a binary carries: the main
// module's when it is ochazuke, else the dependency's (its replacement's
// when replaced). The same rule as catalog inspect's.
func ochazukeOf(bi *debug.BuildInfo) string {
	if strings.HasSuffix(bi.Main.Path, "/ochazuke") {
		if v := bi.Main.Version; v != "(devel)" {
			return v
		}
		return ""
	}
	for _, d := range bi.Deps {
		if strings.HasSuffix(d.Path, "/ochazuke") {
			if d.Replace != nil && d.Replace.Version != "" {
				return d.Replace.Version
			}
			return d.Version
		}
	}
	return ""
}

// FromGit is the stamp for files generated in the checkout at dir: HEAD,
// the nearest tag description as the version, the commit date, and dirty
// when git status shows a change outside the paths in exclude (the output
// directory, so generating twice gives the same stamp). The ochazuke
// version is the running binary's ochazuke dependency, or the checkout's
// own version when the running binary is ochazuke's (cmd/docgen).
func FromGit(dir, tool string, exclude ...string) (Stamp, error) {
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			return "", errors.New("git " + strings.Join(args, " ") + ": " + cmp.Or(strings.TrimSpace(errOut.String()), err.Error()))
		}
		return strings.TrimSpace(out.String()), nil
	}
	s := Stamp{Tool: tool}
	var err error
	if s.Commit, err = run("rev-parse", "HEAD"); err != nil {
		return s, err
	}
	if s.Version, err = run("describe", "--tags", "--always"); err != nil {
		return s, err
	}
	if s.Date, err = run("log", "-1", "--format=%cs"); err != nil {
		return s, err
	}
	status := []string{"status", "--porcelain", "--", "."}
	for _, path := range exclude {
		status = append(status, ":(exclude)"+path)
	}
	changes, err := run(status...)
	if err != nil {
		return s, err
	}
	s.Dirty = changes != ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		if strings.HasSuffix(bi.Main.Path, "/ochazuke") {
			s.Ochazuke = s.Version
		} else {
			s.Ochazuke = ochazukeOf(bi)
		}
	}
	return s, nil
}
