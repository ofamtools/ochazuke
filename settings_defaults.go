package ochazuke

import (
	"embed"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// The framework's settings are rows of settings/ochazuke.json: names,
// types, defaults and descriptions are data, declared from the file
// (declareFrameworkSettings). Go keeps only what JSON cannot hold, the
// checks (settingChecks).
//
//go:embed settings/*.json
var settingFiles embed.FS

// The framework settings the key resolver reads.
const (
	// SequenceTimeoutSetting is how long every pending key sequence (a
	// multi-key row's prefix such as g of g g, and a row entering a
	// pending mode with a prefix, such as the terminal leader) waits for
	// its next key before the shorter binding runs: a duration, 0 (the
	// default) waits until a key comes.
	SequenceTimeoutSetting = "sequence-timeout"
	// SequenceUnknownSetting says what a key no row continues a pending
	// sequence with does: SequencePass (the default) or SequenceSwallow.
	SequenceUnknownSetting = "sequence-unknown"
	// SequencePass ends the sequence, runs its shorter binding (the
	// leader alone: send it to the pane) and handles the key as if it
	// were typed alone.
	SequencePass = "pass"
	// SequenceSwallow drops the sequence and the key, with a notice
	// naming the keys.
	SequenceSwallow = "swallow"
	// RenameSelectSetting opens a rename (outline.edit without select, a
	// RecordSpec with Rename) with the whole text selected (Input.SelectAll):
	// typing replaces it, a motion keeps it. Default on; off opens it with
	// the cursor at the end.
	RenameSelectSetting = "rename.select"
)

// frameworkSettings are the rows of the embedded settings files, each
// default already in its typed form. A file that does not parse is a
// build mistake the settings rule test catches, so it panics.
var frameworkSettings = sync.OnceValue(func() []Setting {
	names, err := settingFiles.ReadDir("settings")
	if err != nil {
		panic(err)
	}
	var out []Setting
	for _, entry := range names {
		data, err := settingFiles.ReadFile("settings/" + entry.Name())
		if err != nil {
			panic(err)
		}
		var rows []Setting
		if err := DecodeJSONC(data, &rows); err != nil {
			panic(fmt.Sprintf("settings/%s: %v", entry.Name(), err))
		}
		for _, one := range rows {
			def, err := coerceSetting(one, one.Default)
			if err != nil {
				panic(fmt.Sprintf("settings/%s: %s default: %v", entry.Name(), one.Name, err))
			}
			one.Default = def
			one.Validate = settingChecks[one.Name]
			out = append(out, one)
		}
	}
	return out
})

// settingChecks are the framework settings' Validate functions by name.
var settingChecks = map[string]func(any) error{
	SequenceTimeoutSetting: nonNegativeDuration,
	TerminalLeaderSetting:  validLeader,
}

// frameworkSetting is one row of the embedded settings: by its name, or
// by a deprecated name it lists.
func frameworkSetting(name string) (Setting, bool) {
	for _, one := range frameworkSettings() {
		if one.Name == name || slices.Contains(one.Deprecated, name) {
			return one, true
		}
	}
	return Setting{}, false
}

// nonNegativeDuration is the check of a duration that 0 turns off.
func nonNegativeDuration(v any) error {
	if d, _ := v.(time.Duration); d < 0 {
		return errors.New(`must be 0 (wait for the next key) or a positive duration such as "1s"`)
	}
	return nil
}

// declareFrameworkSettings declares the named framework settings from the
// embedded rows, each unless the application declared it (or a deprecated
// name of it) first with its own default.
func (r *Runtime) declareFrameworkSettings(names ...string) {
	if r.Config() == nil {
		return
	}
	s := r.Settings()
	for _, name := range names {
		one, ok := frameworkSetting(name)
		if !ok || s.find(one.Name) != nil {
			continue
		}
		_ = r.DeclareSetting(one)
	}
}

// settingValue is a setting's value for the runtime's own reads (the key
// resolver, a binding row naming it): the declared value, its default
// when the value fails the setting's check, or, before the setting is
// declared, the embedded default. False for a name no one declares.
func (r *Runtime) settingValue(name string) (any, bool) {
	if c := r.Config(); c != nil && c.settings != nil {
		s := c.settings
		if one := s.find(s.canonical(name)); one != nil {
			v, _ := s.Value(one.Name)
			if one.Validate != nil && one.Validate(v) != nil {
				v = one.Default
			}
			return v, true
		}
	}
	one, ok := frameworkSetting(name)
	return one.Default, ok
}

// settingText is settingValue as text, in its settings.json form (a
// duration as "1s").
func (r *Runtime) settingText(name string) (string, bool) {
	v, ok := r.settingValue(name)
	if !ok {
		return "", false
	}
	if t, isText := v.(string); isText {
		return t, true
	}
	return fmt.Sprint(fileForm(v)), true
}

// sequenceTimeout is how long a pending sequence waits for its next key:
// BindingTimeout when positive (a host or test that fixes it in code),
// else the sequence-timeout setting. 0 waits until a key comes.
func (r *Runtime) sequenceTimeout() time.Duration {
	if r.BindingTimeout > 0 {
		return r.BindingTimeout
	}
	v, _ := r.settingValue(SequenceTimeoutSetting)
	d, _ := v.(time.Duration)
	return max(d, 0)
}

// swallowsUnknown reports whether sequence-unknown is swallow.
func (r *Runtime) swallowsUnknown() bool {
	v, _ := r.settingText(SequenceUnknownSetting)
	return v == SequenceSwallow
}

// RenameSelect is the rename.select setting: whether a rename opens with
// its text selected. Read at the moment a rename opens.
func (r *Runtime) RenameSelect() bool {
	v, _ := r.settingValue(RenameSelectSetting)
	on, _ := v.(bool)
	return on
}
