package cli

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/unixsuperhero/ochazuke"
)

// CatalogSide declares a program a host may launch beside a pane. Command
// is argv, with {dir}, {pane}, {session}, {project}, {shell} and {bin}
// placeholders. The host supplies these values and owns launching, working
// directory, environment and lifecycle; the library never runs Command.
// Launch defaults to "command"; any other value names a host-defined
// launcher, which may omit Command. Key is an optional single key name.
type CatalogSide struct {
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Key     string   `json:"key,omitempty"`
	Command []string `json:"command,omitempty"`
	Launch  string   `json:"launch,omitempty"`
}

const sidesID = "__catalog-sides"

type sideDeclarations struct {
	Sides []CatalogSide
}

// DeclareSide registers a side on an extension's runtime for Catalog to
// publish, in declaration order. It adds no page, key binding or process.
// Invalid or duplicate declarations leave the runtime unchanged.
func DeclareSide(r *ochazuke.Runtime, side CatalogSide) error {
	if err := checkSide(side); err != nil {
		return err
	}
	var declarations *sideDeclarations
	if inst := r.Instances[sidesID]; inst != nil {
		var ok bool
		declarations, ok = inst.Data.(*sideDeclarations)
		if !ok {
			return fmt.Errorf("side %s: instance %s is already in use", side.Name, sidesID)
		}
		for _, old := range declarations.Sides {
			if old.Name == side.Name {
				return fmt.Errorf("side %s is declared twice", side.Name)
			}
		}
	} else {
		declarations = &sideDeclarations{}
		r.Instance(sidesID, ochazuke.Kind(ochazuke.AppNoun)).Data = declarations
	}
	if side.Launch == "" {
		side.Launch = "command"
	}
	side.Command = slices.Clone(side.Command)
	declarations.Sides = append(declarations.Sides, side)
	return nil
}

func declaredSides(r *ochazuke.Runtime) []CatalogSide {
	if inst := r.Instances[sidesID]; inst != nil {
		if declarations, ok := inst.Data.(*sideDeclarations); ok {
			out := slices.Clone(declarations.Sides)
			for i := range out {
				out[i].Command = slices.Clone(out[i].Command)
			}
			return out
		}
	}
	return nil
}

// ValidateSides checks declarations read from catalog JSON, including name
// uniqueness. An omitted launch means "command". A host must also check
// that it supports any other launcher the catalog names.
func (d CatalogDoc) ValidateSides() error {
	seen := map[string]bool{}
	for _, side := range d.Sides {
		if err := checkSide(side); err != nil {
			return err
		}
		if seen[side.Name] {
			return fmt.Errorf("side %s is declared twice", side.Name)
		}
		seen[side.Name] = true
	}
	return nil
}

var sideName = regexp.MustCompile(`^[a-z0-9-]+$`)

func checkSide(side CatalogSide) error {
	if side.Name == "" {
		return errors.New("side name must be nonempty")
	}
	if !sideName.MatchString(side.Name) {
		return fmt.Errorf("side %q: name must contain only a-z, 0-9 and -", side.Name)
	}
	if side.Key != "" {
		if strings.ContainsFunc(side.Key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return fmt.Errorf("side %s: key must be a single key name", side.Name)
		}
		if _, err := keyEvent(side.Key); err != nil {
			return fmt.Errorf("side %s: %w", side.Name, err)
		}
	}
	if (side.Launch == "" || side.Launch == "command") && len(side.Command) == 0 {
		return fmt.Errorf("side %s: command must be nonempty", side.Name)
	}
	if len(side.Command) > 0 && strings.TrimSpace(side.Command[0]) == "" {
		return fmt.Errorf("side %s: command executable must be nonempty", side.Name)
	}
	if _, err := ExpandSideCommand(side.Command, sideVariables); err != nil {
		return fmt.Errorf("side %s: %w", side.Name, err)
	}
	return nil
}

// sideVariables is also the vocabulary check; empty values validate a
// template without expanding any host context.
var sideVariables = map[string]string{
	"dir": "", "pane": "", "session": "", "project": "", "shell": "", "bin": "",
}

// ExpandSideCommand expands each argv element separately, without shell
// parsing or splitting. Only the six CatalogSide placeholders are allowed.
// Unknown, missing or malformed placeholders are errors; an explicitly
// empty value is valid. Substituted values are never expanded again.
func ExpandSideCommand(cmd []string, vars map[string]string) ([]string, error) {
	out := slices.Clone(cmd)
	for i, arg := range cmd {
		var expanded strings.Builder
		for arg != "" {
			start := strings.IndexAny(arg, "{}")
			if start < 0 {
				expanded.WriteString(arg)
				break
			}
			expanded.WriteString(arg[:start])
			if arg[start] != '{' {
				return nil, fmt.Errorf("side command argument %d: unmatched }", i+1)
			}
			name, tail, ok := strings.Cut(arg[start+1:], "}")
			if !ok {
				return nil, fmt.Errorf("side command argument %d: unmatched {", i+1)
			}
			if _, known := sideVariables[name]; !known {
				return nil, fmt.Errorf("unknown side placeholder {%s}", name)
			}
			value, supplied := vars[name]
			if !supplied {
				return nil, fmt.Errorf("missing side placeholder {%s}", name)
			}
			expanded.WriteString(value)
			arg = tail
		}
		out[i] = expanded.String()
	}
	return out, nil
}
