package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// Accept says what a parameter, a setting or a prop does with a value
// outside its listed choices (its Enum, a Source's rows). No value is
// refused for not being listed (no-refusals D2): it is kept, registered as
// a new choice, or swapped for the one listed name an edit away.
type Accept string

const (
	// AcceptDefault is keep for a parameter and a setting; a prop's
	// default is the choices.user-values setting (register or keep). A
	// field with the default is still drawn as a choice field (a picker).
	AcceptDefault Accept = ""
	// AcceptKeep stores the value as given; a setting's typed getter then
	// answers the declared default, so behaviour stays defined. A field
	// that keeps is text with suggestions (the former Parameter.Free).
	AcceptKeep Accept = "keep"
	// AcceptRegister makes the value a new choice at once (a mode, a
	// status, a tag), so it shows everywhere the list shows.
	AcceptRegister Accept = "register"
	// AcceptNear uses the one listed name a single edit away, with a
	// notice; anything farther is kept.
	AcceptNear Accept = "near"
)

// TakesAny reports a parameter whose value is typed as much as picked
// (Accept is not the default): its form field is text with suggestions,
// not a closed picker, and the palette reads any word for it.
func (p Parameter) TakesAny() bool { return p.Accept != AcceptDefault || p.Free }

// Settings the accept rules read.
const (
	// UserValuesSetting is the default Accept of the props an application
	// declares (its user data lists): register or keep.
	UserValuesSetting = "choices.user-values"
	// FallbackNoticeSetting turns off the status-line notice that says a
	// route, fallback, default or near name ran in place of the request.
	FallbackNoticeSetting = "fallback.notice"
)

// notice tells the user, on the status line's plain tone, what ran in place
// of what was asked, unless fallback.notice is off.
func (r *Runtime) notice(format string, args ...any) {
	if s := r.Settings(); s != nil && s.Has(FallbackNoticeSetting) && !s.Bool(FallbackNoticeSetting) {
		return
	}
	r.UI().Info(fmt.Sprintf(format, args...))
}

// nearOne is the one listed id a single edit from value (case ignored), or
// "" when there is none or more than one.
func nearOne(value string, ids []string) string {
	v := strings.ToLower(value)
	found := ""
	for _, id := range ids {
		if editDistance(v, strings.ToLower(id)) != 1 {
			continue
		}
		if found != "" {
			return ""
		}
		found = id
	}
	return found
}

// acceptValue answers a value outside a listed set of ids under a policy:
// the value to use and whether it should be registered as a new choice.
func acceptValue(policy Accept, value string, ids []string) (use string, register bool) {
	switch policy {
	case AcceptNear:
		if n := nearOne(value, ids); n != "" {
			return n, false
		}
	case AcceptRegister:
		return value, true
	}
	return value, false
}

// acceptArgument handles a string argument outside its parameter's listed
// choices: near swaps the one close name into args (with a notice), and
// register adds the value to the operation's Enum when the choices are the
// Enum (a Source's choices are its verb's to grow). Nothing is refused.
func (r *Runtime) acceptArgument(op Operation, name string, p Parameter, value string, ids []string, args Arguments) {
	use, register := acceptValue(p.Accept, value, ids)
	if use != value {
		args[name] = use
		r.notice("%s: used %s for %s", name, use, value)
		return
	}
	if !register || p.Source != "" || len(p.Enum) == 0 {
		return
	}
	if live, ok := r.Operations.Get(op.Key); ok && live.Parameters != nil {
		lp := live.Parameters[name]
		if !slices.Contains(lp.Enum, value) {
			lp.Enum = append(slices.Clone(lp.Enum), value)
			live.Parameters[name] = lp
		}
	}
}

// propAccept is a prop's policy: its own Accept, else the application's
// choices.user-values setting (register unless set to keep).
func (r *Runtime) propAccept(p PropSpec) Accept {
	if p.Accept != AcceptDefault {
		return p.Accept
	}
	if s := r.Settings(); s != nil && s.Has(UserValuesSetting) && s.String(UserValuesSetting) == string(AcceptKeep) {
		return AcceptKeep
	}
	return AcceptRegister
}

// acceptProp answers a value written to an enum prop that is not one of
// its choices: near gives the one close name, register adds the value to
// the noun's prop Enum (every list of the prop's choices shows it), keep
// stores it as given. The value to write is returned.
func (r *Runtime) acceptProp(noun string, p PropSpec, value any) any {
	s, ok := value.(string)
	if !ok || len(p.Enum) == 0 || slices.Contains(p.Enum, s) {
		return value
	}
	use, register := acceptValue(r.propAccept(p), s, p.Enum)
	if use != s {
		r.notice("%s: used %s for %s", p.Name, use, s)
		return use
	}
	if register {
		if n := r.Nouns[noun]; n != nil {
			for i := range n.Props {
				if n.Props[i].Name == p.Name && !slices.Contains(n.Props[i].Enum, s) {
					n.Props[i].Enum = append(slices.Clone(n.Props[i].Enum), s)
				}
			}
		}
	}
	return s
}

// Has reports whether a setting of that name is declared.
func (s *Settings) Has(name string) bool { return s.find(name) != nil }
