package refdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

// Model is what the reference is drawn from: the declared settings (by
// name) and the default key rows grouped by table (in the bindings file's
// group order). It holds nothing machine-specific: no paths, terminal
// size, theme or installed extensions. The Yours fields carry the user's
// configuration where it differs; they are shown only with
// Options.ShowConfig and are not part of the Fingerprint.
type Model struct {
	Settings []Setting `json:"settings"`
	Bindings []Group   `json:"bindings"`
	// StandardVerbs are the rows of the standard verb table, in its
	// order; Nouns the nouns that declare props, with their store's kind,
	// props and verbs (standard-verbs.md).
	StandardVerbs []StandardVerb `json:"standardVerbs,omitempty"`
	Nouns         []Noun         `json:"nouns,omitempty"`
}

// StandardVerb is one row of the standard verb table: the verb, what it
// does, the store kinds it fits, what it needs of the noun's store and
// props, its aliases, whether it takes a subject (the key prop's
// parameter, filled from the focus), lists rows, runs once over a
// selection (many), repeats with . (a simple-value write), its history
// kind and the confirm it asks by default.
type StandardVerb struct {
	Verb        string   `json:"verb"`
	Description string   `json:"description,omitempty"`
	Kinds       []string `json:"kinds"`
	Needs       string   `json:"needs"`
	Aliases     []string `json:"aliases,omitempty"`
	Subject     bool     `json:"subject,omitempty"`
	Rows        bool     `json:"rows,omitempty"`
	Many        bool     `json:"many,omitempty"`
	Repeatable  bool     `json:"repeatable,omitempty"`
	History     string   `json:"history,omitempty"`
	Confirm     string   `json:"confirm,omitempty"`
}

// Noun is a noun that declares props: its store's kind ("" without a
// store), its props in declaration order and its verbs by name.
type Noun struct {
	Name  string `json:"name"`
	Kind  string `json:"kind,omitempty"`
	Props []Prop `json:"props"`
	Verbs []Verb `json:"verbs,omitempty"`
}

// Prop is one prop of a noun (the PropSpec fields a reader needs): its
// type, the noun a reference names, its allowed values, default, role and
// fallback, and its flags.
type Prop struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Noun        string   `json:"noun,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Role        string   `json:"role,omitempty"`
	Fallback    string   `json:"fallback,omitempty"`
	When        string   `json:"availability,omitempty"`
	Key         bool     `json:"key,omitempty"`
	Required    bool     `json:"required,omitempty"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	CreateOnly  bool     `json:"createOnly,omitempty"`
	FromFocus   bool     `json:"fromFocus,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Multiline   bool     `json:"multiline,omitempty"`
}

// Verb is one verb of a noun with props: its name, the standard verb it
// was generated from ("" for a verb written by hand) and its aliases.
type Verb struct {
	Name     string   `json:"name"`
	Standard string   `json:"standard,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
}

// Setting is one declared setting: its name, type, default (in its file
// form: "400ms" for a duration), allowed values, description, section
// (the title of its section of the Settings page; "" is General) and who declared it.
type Setting struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Default     any      `json:"default"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
	Section     string   `json:"section,omitempty"`
	Framework   bool     `json:"framework,omitempty"`
	// Yours is the user's value in its file form when it differs from
	// the default (settings.json, or a session value), else nil.
	Yours any `json:"-"`
	// YoursFrom says where Yours comes from: "settings.json" or
	// "session".
	YoursFrom string `json:"-"`
}

// Group is one table of default rows: a noun's mode table (Context
// "noun/mode", "noun" for its any-mode table) or an instance's ("instance
// ID" with its mode), the mode it extends, and its rows in table order.
type Group struct {
	Context string `json:"context"`
	Extends string `json:"extends,omitempty"`
	Rows    []Row  `json:"rows"`
	// Yours are the user's rows that change this table, one line each,
	// in the order the user's file applied them.
	Yours []string `json:"-"`
}

// Row is one default key row: its keys (a pattern as "<pattern>"), the
// guard it needs, the action it runs and its arguments (compact JSON),
// what the arguments are taken from, its footer hint, and whether it is
// hidden from the footer.
type Row struct {
	Keys   string `json:"keys"`
	When   string `json:"when,omitempty"`
	Action string `json:"action"`
	Args   string `json:"args,omitempty"`
	From   string `json:"from,omitempty"`
	Flags  string `json:"flags,omitempty"`
	Hint   string `json:"hint,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// Fingerprint is the catalog fingerprint: "sha256:" and the first 12 hex
// digits of the SHA-256 of the model's canonical JSON, the Yours fields
// and tables with no default rows left out. Equal fingerprints mean the
// same reference, whatever commit each side names.
func (m Model) Fingerprint() string {
	// A table only the user's file has rows in is not part of the
	// catalog.
	m.Bindings = slices.DeleteFunc(slices.Clone(m.Bindings), func(g Group) bool { return len(g.Rows) == 0 })
	data, err := json.Marshal(m)
	if err != nil {
		// Only a Default that is not JSON gets here; the root package
		// fills it from a setting's file form, which always is.
		return "sha256:invalid"
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])[:12]
}
