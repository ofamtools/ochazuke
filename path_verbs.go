package ochazuke

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathFreeVerbsSetting turns the free path verbs off: with it off the verbs
// below refuse to run, so a noun with a path prop has only the verbs it
// declares. Default on.
const PathFreeVerbsSetting = "paths.free-verbs"

// The free verbs a noun gets for a prop declared Type "path" (a file, or a
// folder with Path "dir"): one row each in the verb table (legacyVerbs),
// registered by the same code as the standard verbs on every noun that
// has such a prop, never by noun name. A verb the application registers
// under the same name wins.
const (
	pathPropParam = "prop"
	pathWithParam = "with"
)

var needPathProp = storeNeed{Doc: "a prop of Type path", Lacks: "it has no prop of Type path",
	Has: func(_ *Runtime, n *Noun) bool { return len(pathProps(n)) > 0 }}

// pathProps are the noun's props of Type path, in declaration order.
func pathProps(n *Noun) []PropSpec {
	var out []PropSpec
	for _, p := range n.Props {
		if p.IsPath() {
			out = append(out, p)
		}
	}
	return out
}

// IsPath reports whether the parameter (or prop) is a path: PropPath.
func (p Parameter) IsPath() bool { return p.Type == PropPath }

func pathVerbParams(n *Noun) map[string]Parameter {
	names := make([]string, 0, 2)
	for _, p := range pathProps(n) {
		names = append(names, p.Name)
	}
	return map[string]Parameter{
		pathPropParam: {Type: "string", Enum: names, Description: "Which path prop (default: the first, " + names[0] + ")"},
	}
}

func pathOpenWithParams(n *Noun) map[string]Parameter {
	params := pathVerbParams(n)
	params[pathWithParam] = Parameter{Type: "string", Required: true, Free: true, Description: "The application to open it with (macOS: its name, as open -a takes it; elsewhere a command)"}
	return params
}

// thingPath is the path the request's subject holds in its path prop,
// with a leading ~ expanded.
func thingPath(r *Runtime, n *Noun, c Ctx) (string, error) {
	if !r.Settings().Bool(PathFreeVerbsSetting) {
		return "", errors.New("the free path verbs are off (setting paths.free-verbs)")
	}
	props := pathProps(n)
	prop := props[0]
	if name := StringArg(c.Args, pathPropParam, ""); name != "" {
		p := n.prop(name)
		if p == nil || !p.IsPath() {
			return "", &ArgumentError{fmt.Errorf("%s has no path prop %q", n.Name, name)}
		}
		prop = *p
	}
	id, err := r.subjectKey(n, c)
	if err != nil {
		return "", err
	}
	t, err := n.Store.Get(c, id)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", fmt.Errorf("no %s %q", r.DisplayNoun(n.Name), id)
	}
	path := strings.TrimSpace(fmt.Sprint(t[prop.Name]))
	if path == "" || t[prop.Name] == nil {
		return "", fmt.Errorf("%s %q has no %s", r.DisplayNoun(n.Name), id, prop.Name)
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path, nil
}

func pathVerbs() []standardVerb {
	base := standardVerb{Kinds: []string{KindCollection, Singleton, Instances}, Needs: []storeNeed{needGet, needPathProp},
		TakesSubject: true, Params: pathVerbParams, ParamNames: []string{pathPropParam}}
	row := func(verb, description, history, agent string, run func(*Runtime, *Noun, Ctx) (any, error)) standardVerb {
		v := base
		v.Verb, v.Description, v.HistoryKind, v.Agent, v.Run = verb, description, history, agent, run
		return v
	}
	reveal := row("reveal", "Show the thing's path in the file manager (a free verb of every noun with a path prop; setting paths.free-verbs)", HistoryAction, AgentAllow,
		func(r *Runtime, n *Noun, c Ctx) (any, error) {
			p, err := thingPath(r, n, c)
			if err != nil {
				return nil, err
			}
			return p, r.Opener().Reveal(p)
		})
	yank := row("yank-path", "Yank the thing's path into the active register (a free verb of every noun with a path prop)", HistoryChange, AgentAllow,
		func(r *Runtime, n *Noun, c Ctx) (any, error) {
			p, err := thingPath(r, n, c)
			if err != nil {
				return nil, err
			}
			return p, r.storeYank(Yank{Text: p, Shape: "char"}, "", false)
		})
	open := row("open", "Open the thing's path through the opener table (open.openers; the OS by default); a free verb of every noun with a path prop", HistoryAction, AgentConfirm,
		func(r *Runtime, n *Noun, c Ctx) (any, error) {
			p, err := thingPath(r, n, c)
			if err != nil {
				return nil, err
			}
			_, err = r.Open(p)
			return p, err
		})
	with := row("open-with", "Open the thing's path with a named application (a free verb of every noun with a path prop)", HistoryAction, AgentConfirm,
		func(r *Runtime, n *Noun, c Ctx) (any, error) {
			app := strings.TrimSpace(StringArg(c.Args, pathWithParam, ""))
			if app == "" {
				return nil, &ArgumentError{errors.New("open-with needs with, the application")}
			}
			p, err := thingPath(r, n, c)
			if err != nil {
				return nil, err
			}
			return p, r.Opener().OpenWith(app, p)
		})
	with.Params, with.ParamNames = pathOpenWithParams, []string{pathPropParam, pathWithParam}
	with.Positional = func(*Noun) []string { return []string{pathWithParam} }
	return []standardVerb{reveal, yank, open, with}
}
