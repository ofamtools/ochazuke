package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

// Complete returns value completions for words after the binary name. The
// last word is the word being completed (empty after a space); the preceding
// words are parsed exactly as an execution request, including sibling flags.
func (s *Shell) Complete(words []string) ([]ochazuke.Choice, error) {
	if len(words) == 0 {
		return nil, nil
	}
	current := words[len(words)-1]
	before := slices.Clone(words[:len(words)-1])
	noun, rest, ok := s.Runtime.NounWords(before)
	if !ok || len(rest) == 0 {
		return nil, nil
	}
	verb, _, err := s.nounWord(noun, rest[0])
	if err != nil {
		return nil, err
	}
	flags := rest[1:]
	name, prefix := "", ""
	if strings.HasPrefix(current, "--") {
		flag, value, inline := strings.Cut(strings.TrimPrefix(current, "--"), "=")
		if !inline {
			return nil, nil
		}
		name, current, prefix = flag, value, "--"+flag+"="
	} else if len(flags) > 0 && strings.HasPrefix(flags[len(flags)-1], "--") && !strings.Contains(flags[len(flags)-1], "=") {
		name = strings.TrimPrefix(flags[len(flags)-1], "--")
		flags = flags[:len(flags)-1]
	}
	q, _, err := s.request(noun, verb, flags)
	if err != nil {
		return nil, err
	}
	op := s.Runtime.Operations[q.Key()]
	if name == "" {
		for _, candidate := range op.ParameterOrder() {
			if _, set := q.Args[candidate]; !set && op.Parameters[candidate].Type != "boolean" {
				name = candidate
				break
			}
		}
	}
	if _, ok := op.Parameters[name]; !ok {
		return nil, nil
	}
	choices, err := s.Runtime.ParameterChoices(s.Runtime.Context(), op, name, current, q.Args)
	if err != nil {
		return nil, err
	}
	choices = slices.DeleteFunc(choices, func(ch ochazuke.Choice) bool { return !strings.HasPrefix(ch.ID, current) })
	for i := range choices {
		choices[i].ID = prefix + choices[i].ID
	}
	return choices, nil
}

func (s *Shell) completeCommand(_ context.Context, words []string) error {
	if len(words) > 0 && words[0] == "--" {
		words = words[1:]
	}
	choices, err := s.Complete(words)
	if err != nil {
		return err
	}
	for _, ch := range choices {
		fmt.Fprintln(s.stdout(), ch.ID)
	}
	return nil
}

func (s *Shell) completionCommand(_ context.Context, words []string) error {
	if len(words) != 1 {
		return fmt.Errorf("completion takes bash or zsh")
	}
	// Quote the binary name as a shell word; the script invokes this same bin.
	bin := "'" + strings.ReplaceAll(s.Bin, "'", "'\\''") + "'"
	switch words[0] {
	case "bash":
		fmt.Fprintf(s.stdout(), `_ochazuke_complete() {
  local value
  COMPREPLY=()
  while IFS= read -r value; do COMPREPLY+=("$value"); done < <("${COMP_WORDS[0]}" complete -- "${COMP_WORDS[@]:1:COMP_CWORD}")
}
complete -F _ochazuke_complete %s
`, bin)
	case "zsh":
		fmt.Fprintf(s.stdout(), `_ochazuke_complete() {
  local -a values
  values=("${(@f)$("${words[1]}" complete -- "${words[@]:1:$((CURRENT-1))}")}")
  compadd -- "${values[@]}"
}
compdef _ochazuke_complete %s
`, bin)
	default:
		return fmt.Errorf("completion takes bash or zsh")
	}
	return nil
}
