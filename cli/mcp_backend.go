package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdmaps "maps"
	"os"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli/mcp"
)

type mcpBackend struct {
	shell *Shell
	actor ochazuke.Actor
}

// NewMCPServer binds an agent identity from the startup environment. A Shell's
// explicit agent actor takes precedence; an owner identity never makes an MCP
// server act as the owner. The backend keeps this in-process runtime so history
// and exact undo entries survive across calls. It never attaches to a live TUI.
func (s *Shell) NewMCPServer() (*mcp.Server, error) {
	var actor ochazuke.Actor
	if s.Actor != nil {
		actor = *s.Actor
	} else {
		var err error
		actor, err = ochazuke.ActorFromEnv(func(name string) string {
			if name == "OCHAZUKE_ACTOR" && strings.TrimSpace(os.Getenv(name)) == "" {
				return "agent"
			}
			return os.Getenv(name)
		})
		if err != nil {
			return nil, err
		}
	}
	if !actor.IsAgent() {
		actor = ochazuke.Actor{Kind: ochazuke.ActorAgent, Harness: os.Getenv("OCHAZUKE_HARNESS"), Model: os.Getenv("OCHAZUKE_MODEL"), Pane: os.Getenv("TUIOS_PANE_ID"), Session: os.Getenv("TUIOS_SESSION")}
	}
	if actor.Pane == "" && actor.Name == "" {
		actor.Name = s.Bin + "-mcp"
	}
	return s.newMCPServer(actor)
}

func (s *Shell) newMCPServer(actor ochazuke.Actor) (*mcp.Server, error) {
	backend := &mcpBackend{shell: s, actor: actor}
	aliases := slices.Clone(s.MCPAliases)
	for i := range aliases {
		alias := &aliases[i]
		op, err := s.resolveMCPVerb(alias.Verb)
		if err != nil {
			return nil, err
		}

		if alias.Tool.Description == "" {
			alias.Tool.Description = op.Description
		}
		if alias.Tool.InputSchema == nil {
			params := stdmaps.Clone(op.Parameters)
			if a, ok := s.Runtime.ActionAlias(alias.Verb); ok {
				for name := range a.Args {
					p := params[name]
					p.Required = false
					params[name] = p
				}
			}
			alias.Tool.InputSchema = parameterSchema(params)
			if len(alias.Args) > 0 {
				props := alias.Tool.InputSchema["properties"].(map[string]any)
				required, _ := alias.Tool.InputSchema["required"].([]string)
				for from, to := range alias.Args {
					if p, ok := props[to]; ok {
						delete(props, to)
						props[from] = p
					}
					for j, name := range required {
						if name == to {
							required[j] = from
						}
					}
				}
			}
		}
		alias.Tool.Annotations = map[string]any{"readOnlyHint": op.HistoryKindOf() == ochazuke.HistoryFetch, "destructiveHint": op.Destructive, "openWorldHint": true}
	}
	tools := mcp.DefaultTools()
	for _, alias := range aliases {
		if !alias.Hidden {
			tools = append(tools, alias.Tool)
		}
	}
	instructions, err := s.mcpInstructions(actor, tools)
	if err != nil {
		return nil, err
	}
	return mcp.New(backend, mcp.Options{Name: s.Bin, Version: s.version(), Instructions: instructions, Aliases: aliases})
}

func (s *Shell) resolveMCPVerb(name string) (ochazuke.Operation, error) {
	r := s.Runtime
	if op, _, ok := r.ResolveAction(name); ok {
		return op, nil
	}
	candidates := map[string]ochazuke.Operation{}
	match := func(spelling string, op ochazuke.Operation) {
		if strings.HasPrefix(spelling, name) {
			candidates[op.Key()] = op
			return
		}
		noun, verb := ochazuke.SplitKey(name)
		cn, cv := ochazuke.SplitKey(spelling)
		if noun != "" && strings.HasPrefix(cn, noun) && strings.HasPrefix(cv, verb) {
			candidates[op.Key()] = op
		}
	}
	for _, op := range r.Catalog() {
		match(op.Key(), op)
		match(r.DisplayKey(op.Key()), op)
		for _, alias := range r.Aliases() {
			if !alias.Shadowed && alias.Noun == op.Noun {
				match(alias.Alias+"."+op.Verb, op)
			}
		}
		for _, alias := range r.AliasesOf(op.Key()) {
			match(alias.Alias, op)
		}
	}
	if len(candidates) == 1 {
		for _, op := range candidates {
			return op, nil
		}
	}
	if len(candidates) == 0 {
		return ochazuke.Operation{}, fmt.Errorf("unknown verb %q; call catalog", name)
	}
	keys := slices.Sorted(stdmaps.Keys(candidates))
	return ochazuke.Operation{}, fmt.Errorf("ambiguous verb %q: %s", name, strings.Join(keys, ", "))
}

func parameterSchema(params map[string]ochazuke.Parameter) map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, name := range slices.Sorted(stdmaps.Keys(params)) {
		p := params[name]
		kind := p.Type
		switch kind {
		case "boolean", "integer", "number", "array", "object":
		case "list":
			kind = "array"
		default:
			kind = "string"
		}
		field := map[string]any{"type": kind}
		if p.Description != "" {
			field["description"] = p.Description
		}
		if len(p.Enum) > 0 {
			field["enum"] = p.Enum
		}
		if kind == "array" {
			field["items"] = map[string]any{"type": "string"}
		}
		props[name] = field
		if p.Required {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (b *mcpBackend) Call(ctx context.Context, tool string, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch tool {
	case "catalog":
		return b.catalog(args)
	case "describe":
		return b.describe(ochazuke.StringArg(args, "verb", ""))
	case "run":
		return b.run(ctx, args)
	case "state":
		return b.state(args), nil
	}
	return nil, fmt.Errorf("unknown catalog tool %q", tool)
}

func (b *mcpBackend) catalog(args map[string]any) (any, error) {
	s := b.shell
	r := s.Runtime
	raw, err := Catalog(r, s.Bin, s.version(), s.catalogOptions...)
	if err != nil {
		return nil, err
	}
	rights := []ochazuke.AgentRight{}
	for _, op := range r.Catalog() {
		rights = append(rights, r.AgentRight(op, false))
	}
	hashInput, _ := json.Marshal(struct {
		Catalog           json.RawMessage
		Rights            []ochazuke.AgentRight
		IncludeExtensions bool
	}{raw, rights, r.Settings().Bool("mcp.include-extensions")})
	sum := sha256.Sum256(hashInput)
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	noun := ochazuke.StringArg(args, "noun", "")
	if noun != "" {
		noun = r.CanonicalNoun(noun)
	}
	words := strings.Fields(strings.ToLower(ochazuke.StringArg(args, "search", "")))
	include := ochazuke.StringArg(args, "include", "app")
	limit := ochazuke.IntArg(args, "limit", 60)
	offset := ochazuke.IntArg(args, "offset", 0)
	nouns := []map[string]any{}
	total, shown := 0, 0
	for _, n := range doc.Nouns {
		if noun != "" && n.Name != noun {
			continue
		}
		verbs := []map[string]any{}
		for _, v := range n.Verbs {
			op, _ := r.Operation(n.Name + "." + v.Name)
			if op.CatalogProxy && !r.Settings().Bool("mcp.include-extensions") {
				continue
			}
			if include == "app" && v.Builtin || include == "builtin" && !v.Builtin {
				continue
			}
			hay := strings.ToLower(n.Name + " " + n.About + " " + v.Name + " " + v.Description)
			for _, a := range v.Aliases {
				hay += " " + strings.ToLower(a.Alias)
			}
			matches := true
			for _, word := range words {
				if !strings.Contains(hay, word) {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			total++
			if total <= offset || shown >= limit {
				continue
			}
			right := r.AgentRight(op, false)
			verbs = append(verbs, map[string]any{"name": v.Name, "summary": v.Description, "history": v.History, "right": right.Right, "destructive": v.Destructive})
			shown++
		}
		if len(verbs) > 0 {
			nouns = append(nouns, map[string]any{"name": n.Name, "about": n.About, "verbs": verbs})
		}
	}
	more := max(0, total-offset-shown)
	result := map[string]any{"app": s.Bin, "version": s.version(), "hash": "sha256:" + hex.EncodeToString(sum[:]), "live": false, "nouns": nouns, "more": more}
	if more > 0 {
		result["next_offset"] = offset + shown
	}
	return result, nil
}

func (b *mcpBackend) describe(name string) (any, error) {
	s := b.shell
	r := s.Runtime
	op, err := s.resolveMCPVerb(name)
	if err != nil {
		return nil, err
	}
	right := r.AgentRight(op, false)
	params := stdmaps.Clone(op.Parameters)
	for name, p := range params {
		p.Source = r.ChoicesSource(p)
		params[name] = p
	}
	guards := append(r.VerbAvailability(op.Key()), op.CatalogGuards...)
	schema := parameterSchema(params)
	return map[string]any{"verb": op.Key(), "description": op.Description, "params": params, "inputSchema": schema, "required": schema["required"], "positional": op.Positional, "guards": guards, "many": op.Many, "acts": []string{"focus"}, "confirm": op.Confirm, "history": op.HistoryKindOf(), "destructive": op.Destructive, "right": right.Right, "rule": right.Rule, "examples": op.ExamplesOf(), "aliases": r.AliasesOf(op.Key()), "cli": s.commandFor(op, "", op.ExamplesOf()[0].Args)}, nil
}

func (s *Shell) commandFor(op ochazuke.Operation, target string, args map[string]any) string {
	// Shell-quote values for owner copy; MCP never executes this string.
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	parts := []string{}
	for _, word := range s.mcpCommandWords() {
		parts = append(parts, quote(word))
	}
	parts = append(parts, quote(op.Noun), quote(op.Verb))
	if target != "" {
		parts = append(parts, "--target", quote(target))
	}
	raw, _ := json.Marshal(args)
	if args != nil {
		parts = append(parts, "--json", quote(string(raw)))
	}
	return strings.Join(parts, " ")
}

func (b *mcpBackend) run(ctx context.Context, args map[string]any) (any, error) {
	s := b.shell
	r := s.Runtime
	name := ochazuke.StringArg(args, "verb", "")
	op, err := s.resolveMCPVerb(name)
	if err != nil {
		return map[string]any{"status": "failed", "reason": err.Error(), "result": nil, "messages": []ochazuke.UIMessage{}}, nil
	}
	target := ochazuke.StringArg(args, "target", "")
	values, _ := args["args"].(map[string]any)
	answer := map[string]any{"status": "done", "result": nil, "messages": []ochazuke.UIMessage{}}
	if acts := ochazuke.StringArg(args, "acts", "focus"); acts != "focus" {
		answer["status"] = "refused"
		answer["reason"] = "headless sessions have no owner's selection or marks; pass an explicit target"
		return answer, nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	if values == nil {
		raw = []byte("{}")
	}
	flags := []string{"--json", string(raw)}
	if target != "" {
		flags = append(flags, "--target", target)
	}
	// Preserve action aliases and their preset/renamed parameters.
	noun, verb := op.Noun, op.Verb
	command := []string{noun, verb}
	if _, inv, ok := r.ResolveAction(name); ok && inv.Alias {
		if !strings.Contains(inv.Name, ".") {
			noun, verb = "", inv.Name
			command = []string{inv.Name}
		} else {
			noun, verb = inv.Noun, inv.Verb
			command = []string{noun, verb}
		}
	}
	// Parse the same typed request and alias presets as CLI flags do.
	q, _, err := s.request(noun, verb, flags)
	if err != nil {
		answer["status"] = "failed"
		answer["reason"] = err.Error()
		return answer, nil
	}
	if target == "" {
		if owners := s.ownersOf(q.Noun); len(owners) == 1 {
			q.Target = owners[0]
		}
	}
	if dry, _ := args["dry_run"].(bool); dry {
		restore := r.BindActor(b.actor, false)
		err = r.CheckRequest(q)
		restore()
		answer["dry_run"] = true
		answer["right"] = r.AgentRight(op, false)
	} else {
		lastSeq := uint64(0)
		for _, e := range r.HistoryOf(0, ochazuke.HistoryAction, ochazuke.HistoryChange, ochazuke.HistoryFetch) {
			if e.Seq > lastSeq {
				lastSeq = e.Seq
			}
		}
		var out, stderr bytes.Buffer
		clone := *s
		clone.Stdin = os.Stdin
		clone.Stdout = &out
		clone.Stderr = &stderr
		clone.Actor = &b.actor
		clone.Source = "mcp"
		clone.in = nil
		clone.StateCache.Off = true
		messages := []ochazuke.UIMessage{}
		clone.messages = &messages
		err = clone.Run(ctx, append(command, flags...))
		answer["messages"] = messages
		if err == nil {
			var result any
			if parseErr := json.Unmarshal(out.Bytes(), &result); parseErr != nil {
				result = strings.TrimSpace(out.String())
			}
			answer["result"] = result
		}
		// This headless runtime has no place to leave a modal. Clear it after
		// reporting the CLI's "needs the TUI" failure so future calls can run.
		for r.TopModal() != "" {
			if popErr := r.Pop(); popErr != nil {
				break
			}
		}
		entries := r.HistoryOf(0, ochazuke.HistoryAction, ochazuke.HistoryChange, ochazuke.HistoryFetch)
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if e.Seq <= lastSeq || e.Request.Key() != op.Key() || e.Context.Actor != b.actor {
				continue
			}
			answer["entry"] = e.Seq
			undoable := len(e.Changes) > 0 && e.Result.OK
			for _, change := range e.Changes {
				n := r.Nouns[change.Noun]
				if change.Undone || n == nil || n.Store == nil || !n.Store.Undo {
					undoable = false
				}
			}
			if undoable {
				answer["undo"] = map[string]any{"verb": "ochazuke.history.undo", "args": map[string]any{"entry": e.Seq}}
			}
			break
		}
	}
	if err != nil {
		answer["status"] = "failed"
		var refused *ochazuke.RefusedError
		if errors.As(err, &refused) {
			answer["status"] = "refused"
		}
		answer["reason"] = err.Error()
		if r.AgentRight(op, false).Right == ochazuke.AgentConfirm {
			answer["owner_command"] = s.commandFor(op, target, values)
		}
	}
	return answer, nil
}

func (b *mcpBackend) state(args map[string]any) map[string]any {
	r := b.shell.Runtime
	out := map[string]any{"live": false, "app": b.shell.Bin, "page": r.Page, "focus": map[string]any{}, "mode": r.Mode.Name, "modals": []any{}, "selection": nil}
	if inst := r.Instances[r.Focus]; inst != nil {
		out["focus"] = map[string]string{"id": inst.ID, "noun": inst.Noun, "label": inst.Label}
	}
	include, _ := args["include"].([]any)
	if slices.Contains(include, any("history")) {
		entries := r.HistoryOf(0, ochazuke.HistoryAction, ochazuke.HistoryChange, ochazuke.HistoryFetch)
		recent := []ochazuke.HistoryEntry{}
		for _, e := range entries {
			if e.Context.Actor == b.actor {
				recent = append(recent, e)
			}
		}
		limit := ochazuke.IntArg(args, "limit", 20)
		if len(recent) > limit {
			recent = recent[len(recent)-limit:]
		}
		out["recent"] = recent
	}
	return out
}

func (s *Shell) mcpInstructions(actor ochazuke.Actor, tools []mcp.Tool) (string, error) {
	raw, err := Catalog(s.Runtime, s.Bin, s.version(), s.catalogOptions...)
	if err != nil {
		return "", err
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	if !s.Runtime.Settings().Bool("mcp.include-extensions") {
		doc.Extensions = nil
		for i := range doc.Nouns {
			doc.Nouns[i].Verbs = slices.DeleteFunc(doc.Nouns[i].Verbs, func(v CatalogVerb) bool {
				op, _ := s.Runtime.Operation(doc.Nouns[i].Name + "." + v.Name)
				return op.CatalogProxy
			})
		}
	}
	return AgentInstructions(doc, actor, false, tools)
}

// registerMCPVerbs gives discovery the same palette/CLI paths as other fetches.
func (s *Shell) registerMCPVerbs() {
	r := s.Runtime
	if _, exists := r.Operation("ochazuke.catalog.describe"); exists {
		return
	}
	r.Builtin(func() {
		r.Define("ochazuke.agent").Verb("instructions", func(ctx ochazuke.Ctx) ochazuke.Result {
			actor := ctx.Actor()
			server, err := s.newMCPServer(actor)
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(server.Instructions())
		}, "Show generated catalog guidance for agents").Fetch().TextResult()
		for _, name := range []string{"serve", "tools", "instructions", "config"} {
			r.Define("mcp").Verb(name, func(ochazuke.Ctx) ochazuke.Result {
				return ochazuke.Pending(func(ctx context.Context) (any, error) {
					return s.mcpValue(ctx, name)
				})
			}, "MCP "+name).Fetch().Quiet()
		}
		r.Register(ochazuke.Operation{Noun: "ochazuke.catalog", Verb: "list", Description: "Discover nouns, verbs and effective agent rights", HistoryKind: ochazuke.HistoryFetch, Parameters: map[string]ochazuke.Parameter{"noun": {Type: "string"}, "search": {Type: "string"}, "include": {Type: "string", Enum: []string{"app", "builtin", "all"}}, "limit": {Type: "integer"}, "offset": {Type: "integer"}}, Run: func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			return (&mcpBackend{shell: s, actor: r.Actor()}).catalog(a)
		}})
		r.Register(ochazuke.Operation{Noun: "ochazuke.catalog", Verb: "describe", Description: "Describe one verb, its parameters, guards and agent right", HistoryKind: ochazuke.HistoryFetch, Parameters: map[string]ochazuke.Parameter{"verb": {Type: "string", Required: true}}, Run: func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			return (&mcpBackend{shell: s, actor: r.Actor()}).describe(ochazuke.StringArg(a, "verb", ""))
		}})
	})
}

func (s *Shell) mcpCommand(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: %s ochazuke mcp serve|tools|instructions|config", s.Bin)
	}
	value, err := s.mcpValue(ctx, args[0])
	if err != nil || args[0] == "serve" {
		return err
	}
	if text, ok := value.(string); ok {
		_, err = fmt.Fprintln(s.stdout(), text)
		return err
	}
	return s.print(value)
}

func (s *Shell) mcpValue(ctx context.Context, verb string) (any, error) {
	server, err := s.NewMCPServer()
	if err != nil {
		return nil, err
	}
	switch verb {
	case "serve":
		s.Runtime.EnsureHistory()
		defer s.Runtime.Config().Flush()
		in := s.Stdin
		if in == nil {
			in = strings.NewReader("")
		}
		return nil, server.Serve(ctx, in, s.stdout())
	case "tools":
		return server.Tools(), nil
	case "instructions":
		return server.Instructions(), nil
	case "config":
		words := s.mcpCommandWords()
		return map[string]any{"mcpServers": map[string]any{s.Bin: map[string]any{"command": words[0], "args": append(slices.Clone(words[1:]), "ochazuke", "mcp", "serve")}}}, nil
	}
	return nil, fmt.Errorf("unknown mcp command %q", verb)
}

// The shell's sink can retain structured UI messages while printing the same CLI lines.
type mcpSink struct {
	ochazuke.UISink
	messages *[]ochazuke.UIMessage
}

func (s mcpSink) Notify(id ochazuke.NoticeID, text string) {
	*s.messages = append(*s.messages, ochazuke.UIMessage{Kind: ochazuke.UINotice, ID: id, Text: text})
	s.UISink.Notify(id, text)
}
func (s mcpSink) ClearNotice(id ochazuke.NoticeID) {
	*s.messages = append(*s.messages, ochazuke.UIMessage{Kind: ochazuke.UIClearNotice, ID: id})
	s.UISink.ClearNotice(id)
}
func (s mcpSink) Status(text string) {
	*s.messages = append(*s.messages, ochazuke.UIMessage{Kind: ochazuke.UIError, Text: text})
	s.UISink.Status(text)
}
func (s mcpSink) Info(text string) {
	*s.messages = append(*s.messages, ochazuke.UIMessage{Kind: ochazuke.UIInfo, Text: text})
	s.UISink.Info(text)
}

func (s *Shell) mcpCommandWords() []string {
	if len(s.MCPCommand) > 0 {
		return s.MCPCommand
	}
	return []string{s.Bin}
}
