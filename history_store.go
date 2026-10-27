package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// HistoryFile is the action history file in the state directory.
const HistoryFile = "history.jsonl"

// HistorySetting is the declared setting that bounds the history file (and
// the ring while it is persisted): the last N entries are kept; 0 writes
// and reads no file.
const HistorySetting = "history-limit"

// HistoryFetchesSetting is the declared boolean setting that lets the
// history file keep fetches (entries of kind fetch the user did not type
// into the palette). Off by default: a host polling a state verb would
// fill the file and push the user's own entries out of it, and nothing
// reads a fetch back after a restart.
const HistoryFetchesSetting = "history-fetches"

// unpersistedNouns are the nouns whose entries never reach the history
// file: text editing and its plumbing (the input and editor, raw keys,
// counts, registers, macros, yank history, modes), the palette's own line
// editing, and the keys sent to a terminal.
var unpersistedNouns = map[string]bool{
	"input": true, "editor": true, "key": true, "count": true, "register": true, "macro": true,
	"yank-history": true, "mode": true, "palette": true, "terminal": true,
}

// textLikeArgs are the argument names whose values the history file leaves
// out unless the operation sets PersistArgs: what a user typed (args is a
// command line, as command.run takes, which can carry a secret).
var textLikeArgs = []string{"text", "value", "keys", "args", "password", "secret", "token"}

// persistable is the line an entry writes, or false when it writes none.
// The file keeps palette lines (source "palette", failed ones too, so a
// mistyped line can be recalled and fixed) and other completed operations,
// never text editing (unpersistedNouns), and fetches only while
// history-fetches is on; arguments named like text are dropped unless the
// operation opts in.
func (r *Runtime) persistable(e HistoryEntry) (HistoryEntry, bool) {
	if unpersistedNouns[e.Request.Noun] {
		return e, false
	}
	if fetchRing(e) {
		if v, _ := r.Settings().Value(HistoryFetchesSetting); v != true {
			return e, false
		}
	}
	if !e.Result.OK && e.Context.Source != "palette" {
		return e, false
	}
	op, known := r.Operations[e.Request.Key()]
	if !known {
		return e, false
	}
	if len(e.Request.Args) > 0 && !op.PersistArgs {
		args := Arguments{}
		for name, v := range e.Request.Args {
			if !textLike(name) {
				args[name] = v
			}
		}
		e.Request.Args = args
	}
	return e, true
}

func textLike(name string) bool {
	name = strings.ToLower(name)
	for _, t := range textLikeArgs {
		if strings.Contains(name, t) {
			return true
		}
	}
	return false
}

// historyStore appends history lines off the loop: the loop marshals an
// entry and sends the line, one writer goroutine appends it and, when the
// file holds twice the limit, rewrites it with the last limit lines (a temp
// file renamed over it).
type historyStore struct {
	path string
	ops  chan historyOp
	done chan struct{}

	mu       sync.Mutex
	err      error
	reported bool
}

type historyOp struct {
	line     []byte
	limit    int
	setLimit bool
}

// StateDirectory is the application's state directory, "" when there is
// none: StateDir when set, else $XDG_STATE_HOME/<App>, else
// ~/.local/state/<App>. Disable leaves none.
func (c *Config) StateDirectory() string {
	if c.StateDir != "" {
		return c.StateDir
	}
	if c.App == "" {
		return ""
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, c.App)
}

// HistoryPath is the history file, "" when the history is not persisted
// (no state directory, or the application set NoHistoryFile).
func (c *Config) HistoryPath() string {
	dir := c.StateDirectory()
	if dir == "" || c.NoHistoryFile {
		return ""
	}
	return filepath.Join(dir, HistoryFile)
}

// Flush stops the history writer once every line sent to it is written,
// and writes the placements record (NounPlacesFile) and stops its writer.
// The Model calls it on quit; later requests are not persisted.
func (c *Config) Flush() error {
	var err error
	if p := c.places; p != nil {
		err = p.flush(c)
	}
	s := c.history
	if s == nil {
		return err
	}
	c.history = nil
	close(s.ops)
	<-s.done
	return errors.Join(s.failure(), err)
}

// EnsureConfigSettings declares the settings the framework reads from the
// user's settings.json, unless the application declared one of the same
// name first: theme (the colour scheme; its choices are the built-in
// themes and themes.json's, read here and again when the config directory
// changed) and history-limit while the history is persisted. It applies
// terminal-leader when an application declared it (EnsureTerminalLeader). Model.Init
// and the CLI shell call it, so settings set works in both; an
// application may call it earlier (after setting Config.App) to have them
// before the Model starts.
func (r *Runtime) EnsureConfigSettings() {
	c := r.Config()
	if c == nil {
		return
	}
	r.ensureThemeSetting()
	r.ensureDimSettings()
	r.ensureHelpSettings()
	r.applyTerminalLeader()
	s := r.Settings()
	if c.HistoryPath() != "" && s.find(HistorySetting) == nil {
		_ = r.DeclareSetting(Setting{Name: HistorySetting, Type: SettingInteger, Default: HistoryLimit, Description: "Action history entries kept across restarts (0 keeps none)"})
	}
	if c.HistoryPath() != "" && s.find(HistoryFetchesSetting) == nil {
		_ = r.DeclareSetting(Setting{Name: HistoryFetchesSetting, Type: SettingBoolean, Default: false, Description: "Keep data requests (fetches) in the history file too"})
	}
}

// historyLimitSetting is the history-limit value, HistoryLimit when the
// setting is not an integer.
func (r *Runtime) historyLimitSetting() int {
	v, ok := r.Settings().Value(HistorySetting)
	n, isInt := v.(int)
	if !ok || !isInt {
		return HistoryLimit
	}
	return max(n, 0)
}

// EnsureHistory persists the action history, once: it reads the history
// file (the last history-limit entries, marked Restored, before what this
// session already recorded), compacts it when it holds more or has lines
// that do not parse (each such file is one notice), and starts the writer
// every later entry is appended through. Nothing happens without a state
// directory (Config.Disable, a test binary) or with Config.NoHistoryFile.
// Model.Init calls it; the CLI shell does not (a one-shot call is not
// history worth keeping).
func (r *Runtime) EnsureHistory() {
	c := r.Config()
	if c == nil || c.historyStarted {
		return
	}
	c.historyStarted = true
	path := c.HistoryPath()
	if path == "" {
		return
	}
	r.EnsureConfigSettings()
	limit := r.historyLimitSetting()
	r.Settings().Observe(HistorySetting, func(string, any) {
		n := r.historyLimitSetting()
		r.history.limit = n
		if c.history != nil {
			c.history.ops <- historyOp{limit: n, setLimit: true}
		}
	})
	r.history.limit = limit
	var lines [][]byte
	if limit > 0 {
		lines = r.restoreHistory(path, limit)
	}
	s := &historyStore{path: path, ops: make(chan historyOp, 1024), done: make(chan struct{})}
	c.history = s
	go s.run(lines, limit)
}

// restoreHistory reads the file into the ring and returns the lines kept.
func (r *Runtime) restoreHistory(path string, limit int) [][]byte {
	c := r.Config()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		c.report(HistoryFile + ": " + err.Error())
		return nil
	}
	var (
		lines    [][]byte
		entries  []HistoryEntry
		bad      int
		firstBad int
		total    int
	)
	for i, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		total++
		var e HistoryEntry
		if err := json.Unmarshal(line, &e); err != nil || e.Request.Verb == "" {
			bad++
			if firstBad == 0 {
				firstBad = i + 1
			}
			continue
		}
		e.Restored = true
		if e.Kind == "" {
			e.Kind = r.historyKind(e.Request)
		}
		lines = append(lines, line)
		entries = append(entries, e)
	}
	if bad > 0 {
		c.report(fmt.Sprintf("%s: %d line(s) do not parse, first at line %d (skipped)", HistoryFile, bad, firstBad))
	}
	if len(entries) > limit {
		entries, lines = entries[len(entries)-limit:], lines[len(lines)-limit:]
	}
	if len(lines) != total {
		if err := writeHistoryFile(path, lines); err != nil {
			c.report(HistoryFile + ": " + err.Error())
		}
	}
	// The restored entries go before what this session recorded, which is
	// renumbered after them so seq keeps rising; each goes to its ring.
	h := &r.history
	var seq uint64
	for _, e := range entries {
		seq = max(seq, e.Seq)
	}
	current := r.History(0)
	for i := range current {
		seq++
		current[i].Seq = seq
	}
	h.seq = seq
	h.entries, h.fetches = nil, nil
	for _, e := range append(entries, current...) {
		if fetchRing(e) {
			h.fetches = appendBounded(h.fetches, e, HistoryFetchLimit)
		} else {
			h.entries = appendBounded(h.entries, e, h.ringLimit())
		}
	}
	return lines
}

// persist sends one entry to the writer, and reports a write failure once.
func (r *Runtime) persist(entry HistoryEntry) {
	c := r.Config()
	if c == nil || c.history == nil || r.history.limit <= 0 {
		return
	}
	s := c.history
	if err := s.failure(); err != nil {
		s.mu.Lock()
		first := !s.reported
		s.reported = true
		s.mu.Unlock()
		if first {
			c.report(HistoryFile + ": " + err.Error() + " (not saved)")
		}
		return
	}
	entry, ok := r.persistable(entry)
	if !ok {
		return
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	s.ops <- historyOp{line: line}
}

func (s *historyStore) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *historyStore) fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// run is the writer goroutine; lines are the file's lines as restored.
func (s *historyStore) run(lines [][]byte, limit int) {
	defer close(s.done)
	var f *os.File
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	compact := func() {
		if len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
		if f != nil {
			f.Close()
			f = nil
		}
		if err := writeHistoryFile(s.path, lines); err != nil {
			s.fail(err)
		}
	}
	for op := range s.ops {
		if s.failure() != nil {
			continue
		}
		if op.setLimit {
			limit = op.limit
			if limit > 0 && len(lines) > limit {
				compact()
			}
			continue
		}
		if limit <= 0 {
			continue
		}
		if f == nil {
			if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
				s.fail(err)
				continue
			}
			var err error
			if f, err = os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
				s.fail(err)
				continue
			}
		}
		if _, err := f.Write(append(op.line, '\n')); err != nil {
			s.fail(err)
			continue
		}
		lines = append(lines, op.line)
		if len(lines) >= 2*limit {
			compact()
		}
	}
}

// writeHistoryFile replaces the file atomically with lines.
func writeHistoryFile(path string, lines [][]byte) error {
	var buf bytes.Buffer
	for _, line := range lines {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return writeAtomic(path, ".history-*.jsonl", buf.Bytes())
}
