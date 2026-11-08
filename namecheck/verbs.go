package namecheck

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The verb check. Names are data: a verb that goes away is a row in a
// removed list, and everything that still names it (Go, JSON, Markdown,
// a user's bindings.json) is found by one scan, not by searching by hand.
// A reference is a dotted token `noun.verb` whose noun the catalog knows,
// or whose verb a removed row names.

// Removed is one verb that has gone, or is going, from the catalog. Noun
// is the noun it was a verb of in any spelling; empty means the verb of
// every thing noun (the standard verb), which leaves a noun's own verb of
// that name alone (collection.move, a motion, stays). Replacement says what to
// write instead; Since is the release that removes it; Note is free text.
//
// A row with Param is a deprecated parameter, not a removed verb: the verb
// stays, and a request to it that gives Param (with every one of With
// given too) is reported, with Replacement (the parameter to use).
type Removed struct {
	Noun        string   `json:"noun,omitempty"`
	Verb        string   `json:"verb"`
	Param       string   `json:"param,omitempty"`
	With        []string `json:"with,omitempty"`
	Replacement string   `json:"replacement"`
	Since       string   `json:"since,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// verbRows are the rows that remove a verb (no Param).
func verbRows(removed []Removed) []Removed {
	return slices.DeleteFunc(slices.Clone(removed), func(r Removed) bool { return r.Param != "" })
}

// DeprecatedParams are the rows of removed that a request to noun.verb
// giving the arguments named in args matches: a deprecated parameter of
// that verb (of every thing noun's standard verb when the row has no
// noun), given with every one of its With.
func (c *Catalog) DeprecatedParams(noun, verb string, args []string, removed []Removed) []Removed {
	ref := noun + "." + verb
	std, inCatalog := c.Known[ref]
	canon := noun
	if k, ok := c.Nouns[noun]; ok {
		canon = k
	}
	var out []Removed
	for _, r := range removed {
		if r.Param == "" || r.Verb != verb || !slices.Contains(args, r.Param) {
			continue
		}
		if r.Noun != "" {
			rn := r.Noun
			if k, ok := c.Nouns[rn]; ok {
				rn = k
			}
			if rn != canon {
				continue
			}
		} else if !inCatalog || std != verb {
			continue // only the standard verb of that name
		}
		if slices.ContainsFunc(r.With, func(w string) bool { return !slices.Contains(args, w) }) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ParamNote is how a report says a deprecated parameter row.
func (r Removed) ParamNote() string {
	param := r.Param
	if len(r.With) > 0 {
		param += " with " + strings.Join(r.With, " and ")
	}
	msg := fmt.Sprintf("parameter %s of %s is deprecated", param, r.Verb)
	if r.Since != "" {
		msg += " (removed in " + r.Since + ")"
	}
	return msg + "; use " + r.Replacement
}

// Catalog is what the verb check knows, from the application's runtime
// (Runtime.VerbCatalog).
type Catalog struct {
	// Nouns maps every spelling of every noun to its canonical name.
	Nouns map[string]string `json:"nouns"`
	// Generated names the standard verbs the framework generates for a
	// thing noun with a store (Runtime.VerbCatalog probes it). A verb that
	// leaves the table leaves this set. When nil it is read from Known's
	// values.
	Generated map[string]bool `json:"generated,omitempty"`
	// Known holds every valid dotted reference (noun.verb, in each noun
	// spelling; an alias; noun.prop; a setting) and, as its value, the
	// standard verb that generated it ("" for a verb an application or the
	// framework declared itself).
	Known map[string]string `json:"known"`
}

// generates reports whether the catalog's framework still generates a
// standard verb of that name for a thing noun.
func (c *Catalog) generates(verb string) bool {
	if c.Generated == nil {
		c.Generated = map[string]bool{}
		for _, std := range c.Known {
			if std != "" {
				c.Generated[std] = true
			}
		}
	}
	return c.Generated[verb]
}

// Reference states.
const (
	// StateUnknown names a noun's verb the catalog lacks and no row removed.
	StateUnknown = "unknown"
	// StateRemoved names a removed verb the catalog no longer has.
	StateRemoved = "removed"
	// StatePending names a removed verb the catalog still has: it works
	// now and fails at the release that removes it.
	StatePending = "pending"
	// StateDeprecated names a request that gives a deprecated parameter
	// (a Removed row with Param): it works until the row's release.
	StateDeprecated = "deprecated"
)

// Ref is one dotted reference found in text.
type Ref struct {
	Text  string // as written
	Noun  string // as written
	Verb  string
	Known bool // Noun is a noun of the catalog
	Line  int  // 1-based within the text
}

// notVerbs are final segments that make a dotted token a file or host
// name (list.go, bindings.json, ofam.dev), never a verb.
var notVerbs = set([]string{
	"go", "md", "json", "jsonc", "txt", "html", "htm", "yaml", "yml", "sh", "mod", "sum", "work",
	"svg", "gif", "png", "jpg", "css", "js", "ts", "py", "rb", "tsv", "csv", "toml", "tmpl", "lock",
	"com", "dev", "io", "ai", "org", "net", "local", "test", "db", "sql", "log", "tape", "golden",
})

// continuesName reports whether the text after a token carries on a name
// or a path (task.rename/x, a.b_c, a.bC, a.b.C).
func continuesName(rest string) bool {
	b := rest[0]
	if b == '.' && len(rest) > 1 {
		b = rest[1]
		return b == '_' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
	}
	return b == '/' || b == '\\' || b == '_' || b >= 'A' && b <= 'Z'
}

var tokenRE = regexp.MustCompile(`[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)+`)

// wordByte reports whether b continues a name or a path, so a token next
// to it is part of something else (a.b/c, x_y.z, Foo.bar, 1.2).
func wordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '/' || b == '\\' || b == '.' || b == '-' || b == '~' || b == '@' || b == '$'
}

// FindRefs reads text for dotted references. A token counts when its noun
// (the longest dotted prefix that is a noun of c, with the verb the next
// segment) is known, or, with no known noun, the last segment is a verb
// some removed row names. The segments of the verb after the first are
// ignored (dirs.base.add reads as dirs.base).
func (c *Catalog) FindRefs(text string, removed []Removed) []Ref {
	return c.findRefs(text, removed, false)
}

// findRefs is FindRefs; firstOnly reads only a token that starts the text
// (after space): the shape of a reference named on purpose, a whole string
// or code span.
func (c *Catalog) findRefs(text string, removed []Removed, firstOnly bool) []Ref {
	var out []Ref
	rowVerbs := map[string]bool{}
	for _, r := range verbRows(removed) {
		rowVerbs[r.Verb] = true
	}
	for _, loc := range tokenRE.FindAllStringIndex(text, -1) {
		s, e := loc[0], loc[1]
		if firstOnly && strings.TrimSpace(text[:s]) != "" {
			break
		}
		if s > 0 && wordByte(text[s-1]) {
			continue
		}
		if e < len(text) && continuesName(text[e:]) {
			continue
		}
		tok := text[s:e]
		segs := strings.Split(tok, ".")
		if notVerbs[segs[len(segs)-1]] {
			continue
		}
		ref := Ref{Text: tok, Line: 1 + strings.Count(text[:s], "\n")}
		known := false
		for i := len(segs) - 1; i >= 1; i-- {
			if _, ok := c.Nouns[strings.Join(segs[:i], ".")]; ok {
				ref.Noun, ref.Verb, ref.Known = strings.Join(segs[:i], "."), segs[i], true
				known = true
				break
			}
		}
		if !known {
			last := segs[len(segs)-1]
			if !rowVerbs[last] {
				continue
			}
			ref.Noun, ref.Verb = strings.Join(segs[:len(segs)-1], "."), last
		}
		ref.Text = ref.Noun + "." + ref.Verb
		out = append(out, ref)
	}
	return out
}

// Classify says what a reference is: "" when it is fine, else a State and
// the removed row that names it (zero when the state is unknown).
func (c *Catalog) Classify(noun, verb string, removed []Removed) (string, Removed) {
	canon, knownNoun := c.Nouns[noun]
	if !knownNoun {
		canon = noun
	}
	std, inCatalog := c.Known[noun+"."+verb]
	for _, r := range verbRows(removed) {
		if r.Verb != verb {
			continue
		}
		if r.Noun != "" {
			rn := r.Noun
			if k, ok := c.Nouns[rn]; ok {
				rn = k
			}
			if rn != canon {
				continue
			}
			if inCatalog {
				return StatePending, r
			}
			return StateRemoved, r
		}
		// The standard verb of every thing noun. A noun's own verb of the
		// name stays (collection.move). Otherwise the verb works while the
		// framework still generates it, for any noun.
		if inCatalog && std == "" {
			continue
		}
		if c.generates(verb) {
			return StatePending, r
		}
		return StateRemoved, r
	}
	if knownNoun && !inCatalog {
		return StateUnknown, Removed{}
	}
	return "", Removed{}
}

// Hit is one reference the check reports.
type VerbHit struct {
	File, Ref, State, Replacement, Since, Note string
	Line                                       int
}

func (h VerbHit) String() string {
	msg := fmt.Sprintf("%s:%d: %s: %s", h.File, h.Line, h.Ref, h.State)
	switch h.State {
	case StateUnknown:
		msg += " (the catalog has no such verb)"
	case StateDeprecated:
		msg = fmt.Sprintf("%s:%d: %s: %s", h.File, h.Line, h.Ref, h.Note)
	default:
		if h.Since != "" {
			msg += " since " + h.Since
		}
		if h.Replacement != "" {
			msg += "; use " + h.Replacement
		}
	}
	return msg
}

// VerbsConfig is what Verbs needs.
type VerbsConfig struct {
	Catalog Catalog
	Removed []Removed
	// Root is the directory to scan; files are read relative to it.
	Root string
	// Files, when set, are the files to scan (slash paths relative to
	// Root); else the files git tracks under Root (all files when Root is
	// not a repository).
	Files []string
	// Exts are the file extensions scanned; default .go .json .md.
	Exts []string
	// Keys are the JSON keys whose string value names a verb; default
	// action, do, alias, call, via. A request written as a noun and a verb
	// on one line is read too.
	Keys []string
	// UnknownIn are the extensions where a verb the catalog lacks is a
	// finding; default .json. Prose and code say `mode.normal` and
	// `state.working` for things that are not verbs, so there only a
	// removed verb (named by a row) is found; a data file names verbs on
	// purpose. An empty, non-nil list turns the finding off.
	UnknownIn []string
	// Skip are slash-path globs (path.Match, or a directory prefix ending
	// in "/") that are not scanned: generated trees. A testdata or vendor
	// directory at any depth is never scanned.
	Skip []string
	// Allow are references (noun.verb as written) that are accepted
	// wherever they are found: words that read as a verb and are not one.
	Allow []string
}

// VerbReport is what Verbs found.
type VerbReport struct {
	Hits []VerbHit
}

// Violations are the hits that fail a check: unknown and removed
// references. Pending ones and deprecated parameters still work; they are
// the work list.
func (r VerbReport) Violations() []VerbHit {
	return slices.DeleteFunc(slices.Clone(r.Hits), func(h VerbHit) bool { return h.State == StatePending || h.State == StateDeprecated })
}

// Deprecated are the hits on a request giving a deprecated parameter.
func (r VerbReport) Deprecated() []VerbHit {
	return slices.DeleteFunc(slices.Clone(r.Hits), func(h VerbHit) bool { return h.State != StateDeprecated })
}

// Pending are the hits on a removed verb the catalog still has.
func (r VerbReport) Pending() []VerbHit {
	return slices.DeleteFunc(slices.Clone(r.Hits), func(h VerbHit) bool { return h.State != StatePending })
}

// Verbs scans tracked Go, JSON and Markdown for references to a verb the
// catalog lacks or a removed row names. Go is read as its string
// literals and comments; JSON and Markdown as text. A CHANGELOG section
// other than Unreleased is history and is skipped. A request that gives a
// deprecated parameter (a row with Param) on one line of JSON or Go,
// outside tests, is reported as deprecated.
func Verbs(cfg VerbsConfig) (VerbReport, error) {
	exts := cfg.Exts
	if len(exts) == 0 {
		exts = []string{".go", ".json", ".md"}
	}
	files := cfg.Files
	if files == nil {
		var err error
		if files, err = trackedFiles(cfg.Root); err != nil {
			return VerbReport{}, err
		}
	}
	cat := &cfg.Catalog
	unknownIn := cfg.UnknownIn
	if unknownIn == nil {
		unknownIn = []string{".json"}
	}
	keys := cfg.Keys
	if len(keys) == 0 {
		keys = defaultKeys
	}
	allow := set(cfg.Allow)
	var rep VerbReport
	for _, f := range files {
		if !slices.Contains(exts, strings.ToLower(path.Ext(f))) || skipped(f, cfg.Skip) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cfg.Root, filepath.FromSlash(f)))
		if err != nil {
			if os.IsNotExist(err) {
				continue // tracked, deleted in the worktree
			}
			return rep, err
		}
		ext := strings.ToLower(path.Ext(f))
		all, lits := textSegments(f, data, keys)
		seen := map[string]bool{}
		report := func(seg segment, ref Ref) {
			if allow[ref.Text] {
				return
			}
			state, row := cat.Classify(ref.Noun, ref.Verb, cfg.Removed)
			line := seg.line + ref.Line - 1
			key := fmt.Sprintf("%d %s", line, ref.Text)
			if state == "" || seen[key] || state == StateUnknown && !slices.Contains(unknownIn, ext) {
				return
			}
			seen[key] = true
			rep.Hits = append(rep.Hits, VerbHit{File: f, Line: line, Ref: ref.Text, State: state,
				Replacement: row.Replacement, Since: row.Since, Note: row.Note})
		}
		// A removed verb is found anywhere: its row says it is a verb. A
		// verb the catalog lacks is only a finding in UnknownIn files, where a
		// reference is a whole string.
		for _, seg := range all {
			for _, ref := range cat.FindRefs(seg.text, cfg.Removed) {
				if state, _ := cat.Classify(ref.Noun, ref.Verb, cfg.Removed); state != StateUnknown {
					report(seg, ref)
				}
			}
		}
		for _, seg := range lits {
			for _, ref := range cat.findRefs(seg.text, cfg.Removed, true) {
				report(seg, ref)
			}
		}
		if !strings.HasSuffix(f, "_test.go") {
			for _, h := range cat.paramHits(f, data, cfg.Removed) {
				if !allow[h.Ref] {
					rep.Hits = append(rep.Hits, h)
				}
			}
		}
	}
	sort.SliceStable(rep.Hits, func(i, j int) bool {
		a, b := rep.Hits[i], rep.Hits[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return rep, nil
}

// VerbsCheck fails t on each violation of Verbs and logs the pending ones.
func VerbsCheck(t testing.TB, cfg VerbsConfig) {
	t.Helper()
	rep, err := Verbs(cfg)
	if err != nil {
		t.Fatalf("namecheck: verbs: %v", err)
	}
	if p := rep.Pending(); len(p) > 0 {
		t.Logf("namecheck: %d references to verbs still in the catalog that a release removes", len(p))
	}
	if d := rep.Deprecated(); len(d) > 0 {
		t.Logf("namecheck: %d requests give a deprecated parameter", len(d))
	}
	v := rep.Violations()
	if len(v) == 0 {
		return
	}
	var sb strings.Builder
	for _, h := range v {
		sb.WriteString(h.String() + "\n")
	}
	t.Fatalf("namecheck: %d references to verbs the catalog lacks\n%s", len(v), sb.String())
}

func skipped(file string, skip []string) bool {
	for _, el := range strings.Split(file, "/") {
		if el == "testdata" || el == "vendor" {
			return true // fixtures hold bad references on purpose
		}
	}
	for _, s := range skip {
		if dir, ok := strings.CutSuffix(s, "/"); ok {
			if file == dir || strings.HasPrefix(file, s) {
				return true
			}
			continue
		}
		if ok, _ := path.Match(s, file); ok {
			return true
		}
	}
	return false
}

// trackedFiles is `git ls-files` under root; every file when root is not a
// repository.
func trackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	if out, err := cmd.Output(); err == nil {
		var files []string
		for _, f := range bytes.Split(out, []byte{0}) {
			if len(f) > 0 {
				files = append(files, string(f))
			}
		}
		return files, nil
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if p != root && strings.HasPrefix(e.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// segment is text to scan, starting at a line of its file.
type segment struct {
	text string
	line int
}

// textSegments are the parts of a file the check reads: all is the text
// a removed verb is looked for in; lits are the strings and code spans a
// reference is named in on purpose.
func textSegments(file string, data []byte, keys []string) (all, lits []segment) {
	switch strings.ToLower(path.Ext(file)) {
	case ".go":
		return goSegments(file, data)
	case ".json":
		lits = jsonStrings(data, keys)
		return []segment{{string(data), 1}}, lits
	case ".md":
		if strings.HasPrefix(strings.ToUpper(path.Base(file)), "CHANGELOG") {
			all = changelogSegments(data)
		} else {
			all = []segment{{string(data), 1}}
		}
		for _, seg := range all {
			lits = append(lits, markdownLiterals(seg)...)
		}
		return all, lits
	}
	return []segment{{string(data), 1}}, nil
}

var (
	jsonPairRE  = regexp.MustCompile(`"([^"\\\n]+)"\s*:\s*"((?:[^"\\\n]|\\.)*)"`)
	nounVerbRE  = regexp.MustCompile(`"noun"\s*:\s*"([^"\n]*)"[^{}\n]*"verb"\s*:\s*"([^"\n]*)"`)
	verbNounRE  = regexp.MustCompile(`"verb"\s*:\s*"([^"\n]*)"[^{}\n]*"noun"\s*:\s*"([^"\n]*)"`)
	codeSpanRE  = regexp.MustCompile("`([^`\\n]+)`")
	defaultKeys = []string{"action", "do", "alias", "call", "via"}
)

// jsonStrings are the values of a JSON (or JSONC) file that name a verb on
// purpose: the string under one of keys (an action, a hook's do), and a
// request written as a noun and a verb on one line.
func jsonStrings(data []byte, keys []string) []segment {
	var out []segment
	text := string(data)
	line := func(at int) int { return 1 + strings.Count(text[:at], "\n") }
	for _, loc := range jsonPairRE.FindAllStringSubmatchIndex(text, -1) {
		if slices.Contains(keys, text[loc[2]:loc[3]]) {
			out = append(out, segment{text[loc[4]:loc[5]], line(loc[0])})
		}
	}
	for _, loc := range nounVerbRE.FindAllStringSubmatchIndex(text, -1) {
		out = append(out, segment{text[loc[2]:loc[3]] + "." + text[loc[4]:loc[5]], line(loc[0])})
	}
	for _, loc := range verbNounRE.FindAllStringSubmatchIndex(text, -1) {
		out = append(out, segment{text[loc[4]:loc[5]] + "." + text[loc[2]:loc[3]], line(loc[0])})
	}
	return out
}

// markdownLiterals are a section's inline code spans and the lines of its
// fenced blocks.
func markdownLiterals(seg segment) []segment {
	var out []segment
	fenced := false
	for i, line := range strings.Split(seg.text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			out = append(out, segment{strings.TrimPrefix(strings.TrimSpace(line), "$ "), seg.line + i})
			continue
		}
		for _, m := range codeSpanRE.FindAllStringSubmatch(line, -1) {
			out = append(out, segment{m[1], seg.line + i})
		}
	}
	return out
}

// goSegments are a Go file's string literals and comments (all) and its
// string literals (lits).
func goSegments(file string, data []byte) (all, lits []segment) {
	fset := token.NewFileSet()
	f := fset.AddFile(file, fset.Base(), len(data))
	var s scanner.Scanner
	s.Init(f, data, func(token.Position, string) {}, scanner.ScanComments)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return all, lits
		}
		if tok != token.STRING && tok != token.COMMENT {
			continue
		}
		text := lit
		if tok == token.STRING {
			if u, err := strconv.Unquote(lit); err == nil {
				text = u
			}
		}
		seg := segment{text, fset.Position(pos).Line}
		all = append(all, seg)
		if tok == token.STRING {
			lits = append(lits, seg)
		}
	}
}

var (
	headingRE    = regexp.MustCompile(`^##\s`)
	unreleasedRE = regexp.MustCompile(`(?i)^##\s+unreleased\b`)
)

// changelogSegments are a changelog's lines outside its history: the
// preamble and Unreleased. Every other section (dated, versioned, or
// historical notes) is history.
func changelogSegments(data []byte) []segment {
	var out []segment
	history := false
	start := 0
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, segment{strings.Join(cur, "\n"), start})
		}
		cur = nil
	}
	for i, line := range strings.Split(string(data), "\n") {
		if headingRE.MatchString(line) {
			flush()
			history = !unreleasedRE.MatchString(line)
		}
		if history {
			continue
		}
		if cur == nil {
			start = i + 1
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

var (
	// A request named on one line of JSON or Go: the action string of a
	// row ("action"/"do"/"call": "noun.verb"), a "noun"/"verb" pair
	// (nounVerbRE), or Go's Noun: and Verb: fields.
	actionRefRE = regexp.MustCompile(`"(?:action|do|call)"\s*:\s*"([a-z][a-z0-9.-]*)`)
	goNounRE    = regexp.MustCompile(`\bNoun:\s*"([^"\n]*)"`)
	goVerbRE    = regexp.MustCompile(`\bVerb:\s*"([^"\n]*)"`)
	argKeyRE    = regexp.MustCompile(`"([a-z][a-zA-Z0-9_-]*)"\s*:`)
)

// paramHits are the lines of a JSON or Go file that name a request and,
// on the same line, give a deprecated parameter of its verb (a key of its
// arguments). A line is the unit: a binding row, an alias row, a Request
// literal.
func (c *Catalog) paramHits(file string, data []byte, removed []Removed) []VerbHit {
	ext := strings.ToLower(path.Ext(file))
	if ext != ".json" && ext != ".go" || !slices.ContainsFunc(removed, func(r Removed) bool { return r.Param != "" }) {
		return nil
	}
	var out []VerbHit
	for i, line := range strings.Split(string(data), "\n") {
		var refs [][2]string
		for _, m := range actionRefRE.FindAllStringSubmatch(line, -1) {
			if n, v, ok := cutLast(m[1]); ok {
				refs = append(refs, [2]string{n, v})
			}
		}
		if ext == ".go" {
			if n, v := goNounRE.FindStringSubmatch(line), goVerbRE.FindStringSubmatch(line); n != nil && v != nil {
				refs = append(refs, [2]string{n[1], v[1]})
			}
		} else {
			for _, m := range nounVerbRE.FindAllStringSubmatch(line, -1) {
				refs = append(refs, [2]string{m[1], m[2]})
			}
			for _, m := range verbNounRE.FindAllStringSubmatch(line, -1) {
				refs = append(refs, [2]string{m[2], m[1]})
			}
		}
		if len(refs) == 0 {
			continue
		}
		var keys []string
		for _, m := range argKeyRE.FindAllStringSubmatch(line, -1) {
			keys = append(keys, m[1])
		}
		for _, ref := range refs {
			for _, row := range c.DeprecatedParams(ref[0], ref[1], keys, removed) {
				out = append(out, VerbHit{File: file, Line: i + 1, Ref: ref[0] + "." + ref[1], State: StateDeprecated,
					Replacement: row.Replacement, Since: row.Since, Note: row.ParamNote()})
			}
		}
	}
	return out
}

// cutLast splits noun.verb at its last dot.
func cutLast(ref string) (string, string, bool) {
	i := strings.LastIndex(ref, ".")
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}
