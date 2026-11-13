package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"ofam.dev/ochazuke"
)

// Host is an extension's sanctioned path back through the host's rights and
// history boundary. Command includes the executable and every global argument
// (notably --data). Env is complete, or nil to inherit the process environment.
type Host struct {
	Command []string
	Caller  string
	Env     []string
}

// HostPeer is the compact extension.peers response (application verbs only).
type HostPeer struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Version   string         `json:"version,omitempty"`
	Nouns     []HostPeerNoun `json:"nouns"`
}

type HostPeerNoun struct {
	Noun  string         `json:"noun"`
	Local string         `json:"local"`
	About string         `json:"about,omitempty"`
	Verbs []HostPeerVerb `json:"verbs"`
}

type HostPeerVerb struct {
	Verb        string                        `json:"verb"`
	Description string                        `json:"description,omitempty"`
	Params      map[string]ochazuke.Parameter `json:"params,omitempty"`
	Agent       string                        `json:"agent"`
}

// HostFromEnv reads JSON argv from OCHAZUKE_HOST. Unset is standalone (ok=false).
// Malformed declarations are errors, never a fallback to a host from PATH.
// OCHAZUKE_EXTENSION supplies the caller's registered namespace; otherwise the
// executable's stable ExtensionNamespace is used.
func HostFromEnv() (host Host, ok bool, err error) {
	return hostFromEnv(os.Getenv, os.Args[0])
}

func hostFromEnv(getenv func(string) string, bin string) (Host, bool, error) {
	raw := strings.TrimSpace(getenv("OCHAZUKE_HOST"))
	if raw == "" {
		return Host{}, false, nil
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil || len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Host{}, false, fmt.Errorf("OCHAZUKE_HOST must be a nonempty JSON argv")
	}
	caller := strings.TrimSpace(getenv("OCHAZUKE_EXTENSION"))
	if caller == "" {
		var err error
		caller, err = OwnNamespace(bin)
		if err != nil {
			return Host{}, false, fmt.Errorf("host caller: %w", err)
		}
	}
	return Host{Command: argv, Caller: caller}, true, nil
}

// Peers asks the host for the compact peer list. A standalone zero Host has no
// peers; a configured host failure is returned rather than hidden.
func (h Host) Peers(ctx context.Context) ([]HostPeer, error) {
	if len(h.Command) == 0 {
		return []HostPeer{}, nil
	}
	raw, err := h.Run(ctx, "extension.peers", nil)
	if err != nil {
		return nil, err
	}
	var peers []HostPeer
	if err := json.Unmarshal(raw, &peers); err != nil {
		return nil, fmt.Errorf("host peers: %w", err)
	}
	return peers, nil
}

// Run invokes an operation through the host, never a peer executable. A key
// uses noun.verb spelling. Use Call for an explicitly dotted verb name.
func (h Host) Run(ctx context.Context, key string, args ochazuke.Arguments) ([]byte, error) {
	noun, verb := ochazuke.SplitKey(key)
	if noun == "" || verb == "" {
		return nil, fmt.Errorf("host operation must be noun.verb: %q", key)
	}
	return h.Call(ctx, noun, verb, args)
}

// Call is Run with the noun/verb boundary explicit (verbs may contain dots).
func (h Host) Call(ctx context.Context, noun, verb string, args ochazuke.Arguments) ([]byte, error) {
	if len(h.Command) == 0 || h.Command[0] == "" {
		return nil, fmt.Errorf("no ochazuke host is configured")
	}
	if strings.TrimSpace(h.Caller) == "" {
		return nil, fmt.Errorf("host call needs an extension caller")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	if args == nil {
		raw = []byte("{}")
	}
	env := h.Env
	if env == nil {
		env = os.Environ()
	}
	lookup := func(key string) string {
		for i := len(env) - 1; i >= 0; i-- {
			if value, ok := strings.CutPrefix(env[i], key+"="); ok {
				return value
			}
		}
		return ""
	}
	depth, chain, err := hostCallContext(lookup)
	if err != nil {
		return nil, err
	}
	chain = append(chain, h.Caller)
	encoded, _ := json.Marshal(chain)
	cmdEnv := slices.Clone(env)
	for key, value := range map[string]string{"OCHAZUKE_CALLER": h.Caller, "OCHAZUKE_HOST_DEPTH": strconv.Itoa(depth + 1), "OCHAZUKE_HOST_CHAIN": string(encoded)} {
		cmdEnv = slices.DeleteFunc(cmdEnv, func(entry string) bool { return strings.HasPrefix(entry, key+"=") })
		cmdEnv = append(cmdEnv, key+"="+value)
	}
	argv := append(slices.Clone(h.Command[1:]), noun, verb, "--json", string(raw))
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// A served tool asks over its pipe (host.run): no host process starts.
	if answer, piped, err := pipeHostRun(ctx, append([]string{h.Command[0]}, argv...), cmdEnv); piped {
		if err == nil && answer.Exit != 0 {
			err = errors.New(cmp.Or(strings.TrimSpace(string(answer.Stderr)), fmt.Sprintf("exit status %d", answer.Exit)))
		}
		if err != nil {
			return nil, fmt.Errorf("host %s.%s: %w", noun, verb, err)
		}
		return answer.Stdout, nil
	}
	cmd := exec.CommandContext(ctx, h.Command[0], argv...)
	cmd.Env = cmdEnv
	cmd.WaitDelay = 5 * time.Second
	stop := capturedGroup(cmd)
	defer stop()
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	// Nil Stdin is closed input. Never add --yes: headless confirmations refuse.
	if err := cmd.Run(); err != nil {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = err.Error()
		}
		return nil, fmt.Errorf("host %s.%s: %s", noun, verb, reason)
	}
	return out.Bytes(), nil
}

// HostCommand runs a command line of the host (argv[0] names the host's
// program, as OCHAZUKE_HOST's first word does, compared by base name) over
// the served pipe when this process is served by that host (host.run), and
// any other command line, or every one when not served, as a process of its
// own. It answers stdout; a failure carries stderr's last line.
func HostCommand(ctx context.Context, argv []string) ([]byte, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("empty command")
	}
	if host, ok, _ := HostFromEnv(); ok && filepath.Base(host.Command[0]) == filepath.Base(argv[0]) {
		if answer, piped, err := pipeHostRun(ctx, argv, os.Environ()); piped {
			if err == nil && answer.Exit != 0 {
				err = errors.New(cmp.Or(lastLine(answer.Stderr), fmt.Sprintf("exit status %d", answer.Exit)))
			}
			if err != nil {
				return nil, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
			}
			return answer.Stdout, nil
		}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = 500 * time.Millisecond
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %s", strings.Join(argv, " "), cmp.Or(lastLine(stderr.Bytes()), err.Error()))
	}
	return out.Bytes(), nil
}

func lastLine(b []byte) string {
	text := strings.TrimSpace(string(b))
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		text = text[i+1:]
	}
	return text
}

func hostCallContext(getenv func(string) string) (int, []string, error) {
	depth := 0
	if raw := getenv("OCHAZUKE_HOST_DEPTH"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, nil, fmt.Errorf("invalid OCHAZUKE_HOST_DEPTH %q", raw)
		}
		depth = n
	}
	var chain []string
	if raw := getenv("OCHAZUKE_HOST_CHAIN"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &chain); err != nil {
			return 0, nil, fmt.Errorf("invalid OCHAZUKE_HOST_CHAIN: %w", err)
		}
	}
	return depth, chain, nil
}

func (s *Shell) hostCallBoundary(actor ochazuke.Actor) (ochazuke.Actor, error) {
	caller := strings.TrimSpace(os.Getenv("OCHAZUKE_CALLER"))
	if caller == "" {
		return actor, nil
	}
	depth, chain, err := hostCallContext(os.Getenv)
	if err != nil {
		return actor, err
	}
	limit := 4
	if value, ok := s.Runtime.Settings().Value("extensions.host-call-depth"); ok {
		if n, ok := value.(int); ok {
			limit = max(0, n)
		}
	}
	if depth > limit {
		return actor, fmt.Errorf("host call depth %d > %d: %s", depth, limit, strings.Join(chain, " -> "))
	}
	if actor.Harness != "extension" || actor.Name != caller || actor.Via == nil {
		actor = actor.ThroughExtension(caller)
	}
	// Only an explicitly protected owner setting may opt into caller rights.
	// Detection has already narrowed Via, so this cannot demote a detected agent.
	if setting, err := s.Runtime.Settings().Get("extensions.host-call-rights"); err == nil && setting.Protected && setting.Value == "caller" {
		actor = *actor.Via
	}
	s.Runtime.EnsureHistory()
	return actor, nil
}
