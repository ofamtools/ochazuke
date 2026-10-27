package ochazuke

import (
	"errors"
	"slices"
	"strings"
)

// configHook is one application file config.reload reads after the
// framework's: its name (the problem prefix), the function and the
// problems its last run reported, withdrawn before the next.
type configHook struct {
	name     string
	fn       func() []string
	problems []string
}

// OnConfigReload adds an application's own configuration to config.reload:
// fn reads its file again and returns the problems it found (an entry
// skipped, a file that does not parse), never fatal. Hooks run in
// registration order after the framework's files. Each problem not already
// starting with "name: " gets that prefix, is reported through
// Config.Report like a framework file's and withdrawn at the next
// config.reload. fn runs on the update loop.
func (r *Runtime) OnConfigReload(name string, fn func() []string) {
	c := r.Config()
	c.hooks = append(c.hooks, &configHook{name: name, fn: fn})
}

// ReloadConfig reads every configuration file the application uses again,
// in dependency order: themes.json (when it was read: the theme setting's
// choices are then current), settings.json (when settings are declared;
// observers see every value that changed, the Model's theme among them),
// actions.json (when it was installed), bindings.json (when it was
// applied, once: a row can name an action's verb), then each
// OnConfigReload hook. Each file's last problems are withdrawn and its new
// ones reported, so no stale notice survives; a bad file is skipped, never
// fatal. It returns the problems in that order, each naming its file, and
// an error only when the application has no config directory.
func (r *Runtime) ReloadConfig() ([]string, error) {
	c := r.Config()
	if c == nil || c.Directory() == "" {
		return nil, errors.New("this application has no config directory")
	}
	problems := []string{}
	if c.themesLoaded {
		problems = append(problems, r.reloadThemes()...)
	}
	if s := r.Settings(); len(s.declared) > 0 {
		problems = append(problems, s.Reload()...)
	}
	if c.actionsLoaded {
		problems = append(problems, r.ReloadUserActions()...)
	}
	if c.bindingsLoaded {
		problems = append(problems, r.ReloadUserBindings()...)
	}
	for _, h := range c.hooks {
		if c.Withdraw != nil {
			for _, p := range h.problems {
				c.Withdraw(p)
			}
		}
		h.problems = nil
		for _, p := range h.fn() {
			if !strings.HasPrefix(p, h.name+": ") {
				p = h.name + ": " + p
			}
			h.problems = append(h.problems, p)
			c.report(p)
		}
		problems = append(problems, h.problems...)
	}
	return problems, nil
}

// reloadThemes is ReloadThemes and then whatever draws the schemes (the
// appearance) taking the new ones.
func (r *Runtime) reloadThemes() []string {
	problems := r.ReloadThemes()
	for _, fn := range r.Config().themeListeners {
		fn()
	}
	return slices.Clone(problems)
}

func registerConfigReload(r *Runtime) {
	r.Register(Operation{Verb: "reload", Noun: "config", Description: "Read every config file again: themes.json, settings.json, actions.json, bindings.json, then the application's own; a bad file or entry is skipped and reported", Result: "problem[]", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		problems, err := r.ReloadConfig()
		if err != nil {
			return nil, err
		}
		return problems, nil
	}})
}
