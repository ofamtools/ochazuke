package ochazuke

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	AgentRightsSetting             = "agents.rights"
	AgentRightsGateSetting         = "agents.rights-gate"
	AgentConfirmAttributionSetting = "agents.confirm-attribution"
)

// AgentRule is an ordered owner override. Match is noun.verb, noun.* or *.
// Where is any (including empty), live or headless.
type AgentRule struct {
	Match string `json:"match"`
	Right string `json:"right"`
	Where string `json:"where,omitempty"`
}

// AgentRight names both the effective policy and its source.
type AgentRight struct {
	Verb  string `json:"verb"`
	Right string `json:"right"`
	Rule  string `json:"rule"`
}

// RefusedError is a policy refusal, modelled on matomeru's work.RefusedError.
// Rule tells the owner which declaration or setting decided it.
type RefusedError struct{ Verb, Rule, Reason string }

func (e *RefusedError) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Verb, e.Reason, e.Rule) }

// Framework policy is data, applied at registration, including late verbs.
var frameworkAgentRights = map[string]string{
	"ochazuke.settings": AgentNever, "ochazuke.setting": AgentNever,
	"ochazuke.setting-item": AgentNever, "ochazuke.setting-value": AgentNever,
	"ochazuke.settings-section": AgentNever, "ochazuke.settings-tab": AgentNever,
	"ochazuke.config": AgentNever, "ochazuke.bindings": AgentNever,
	"ochazuke.actions": AgentNever, "ochazuke.themes": AgentNever,
	"ochazuke.confirm": AgentNever,
}

func parseAgentRules(value any) ([]AgentRule, error) {
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("must be a JSON array of agent rights rows")
	}
	var rows []AgentRule
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rows); err != nil {
		return nil, fmt.Errorf("agent rights: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("agent rights must be one JSON array")
	}
	if strings.TrimSpace(text) == "null" {
		return nil, fmt.Errorf("agent rights must be an array")
	}
	for _, row := range rows {
		if row.Match == "" || strings.ContainsAny(row.Match, " \t\n") || (strings.Contains(row.Match, "*") && row.Match != "*" && (!strings.HasSuffix(row.Match, ".*") || strings.Count(row.Match, "*") != 1)) {
			return nil, fmt.Errorf("invalid agent rights match %q", row.Match)
		}
		switch row.Right {
		case AgentAllow, AgentConfirm, AgentNever:
		default:
			return nil, fmt.Errorf("invalid agent right %q", row.Right)
		}
		switch row.Where {
		case "", "any", "live", "headless":
		default:
			return nil, fmt.Errorf("invalid agent rights where %q", row.Where)
		}
	}
	return rows, nil
}

func registerAgentRights(r *Runtime) {
	// Like list.filters, structured rows are a validated JSON string setting.
	if err := r.DeclareSetting(Setting{Name: AgentRightsSetting, Type: SettingString, Default: "[]", Description: "Ordered JSON rows {match, right, where}: noun.verb, noun.* or *; allow, confirm or never; any, live or headless", Validate: func(v any) error { _, err := parseAgentRules(v); return err }}); err != nil {
		panic(err)
	}
	r.Register(Operation{Noun: "ochazuke.agent", Verb: "rights", Description: "List effective agent rights and the rule that decided each", HistoryKind: HistoryFetch, Parameters: map[string]Parameter{"verb": {Type: "string"}}, Run: func(r *Runtime, c Context, args Arguments) (any, error) {
		rows := []AgentRight{}
		for _, op := range r.Catalog() {
			if key := StringArg(args, "verb", ""); key == "" || r.CanonicalKey(key) == op.Key() {
				rows = append(rows, r.AgentRight(op, r.agentLive()))
			}
		}
		return rows, nil
	}})
}

// AgentRight resolves owner rows before the operation's declaration/default.
func (r *Runtime) AgentRight(op Operation, live bool) AgentRight {
	out := AgentRight{Verb: op.Key(), Right: op.AgentRightOf(live), Rule: "derived default"}
	if op.Agent != "" {
		out.Rule = "verb declaration"
	}
	rows, _ := parseAgentRules(r.Settings().String(AgentRightsSetting))
	for i, row := range rows {
		if row.Where == "live" && !live || row.Where == "headless" && live {
			continue
		}
		match := row.Match
		if match != "*" {
			if strings.HasSuffix(match, ".*") {
				match = r.CanonicalNoun(strings.TrimSuffix(match, ".*")) + ".*"
			} else {
				match = r.CanonicalKey(match)
			}
		}
		if match == "*" || match == op.Key() || strings.HasSuffix(match, ".*") && strings.TrimSuffix(match, ".*") == op.Noun {
			out.Right, out.Rule = row.Right, fmt.Sprintf("%s[%d] %s", AgentRightsSetting, i, row.Match)
			break
		}
	}
	return out
}

// BindActor fixes a transport's identity for all requests, including nested
// requests. Request.Actor is ignored during the scope. In-process callers may
// use WithActor or Request.Actor when no transport is bound. live distinguishes
// a UI loop from a headless shell; this is not an authentication boundary.
func (r *Runtime) BindActor(actor Actor, live bool) (restore func()) {
	before := r.agentTransport
	r.agentTransport = &agentTransport{actor: actor.normalized(), live: live}
	return func() { r.agentTransport = before }
}

type agentTransport struct {
	actor Actor
	live  bool
}

func (r *Runtime) agentLive() bool { return r.agentTransport == nil || r.agentTransport.live }

func (r *Runtime) agentVerdict(c Context, q Request, op Operation) Verdict {
	if !r.Actor().IsAgent() || !r.Settings().Bool(AgentRightsGateSetting) {
		return Allow()
	}
	right := r.AgentRight(op, r.agentLive())
	refuse := func(reason string) Verdict {
		err := &RefusedError{Verb: op.Key(), Rule: right.Rule, Reason: reason}
		return Verdict{Refuse: err.Error(), err: err}
	}
	switch right.Right {
	case AgentNever:
		return refuse("agents may never run this verb; ask the owner")
	case AgentConfirm:
		if q.agentApproved {
			return Allow()
		}
		if !r.agentLive() {
			return refuse("needs the owner's confirmation; ask them to run this command")
		}
		prompt := op.Confirm
		if op.ConfirmFor != nil {
			prompt = op.ConfirmFor(r, c, q, prompt)
		}
		if strings.TrimSpace(prompt) == "" {
			prompt = "Run " + r.DisplayKey(op.Key()) + "?"
		}
		return AskFirst(prompt)
	}
	return Allow()
}

// agentActivity describes the newest active job, or a pending owner question.
func (r *Runtime) agentActivity() string {
	if !r.Settings().Bool("agents.status-segment") || !r.SegmentsShown() {
		return ""
	}
	if inst := r.Instances[r.TopModal()]; inst != nil && inst.Payload.Request != nil && inst.Payload.Request.Actor != nil && inst.Payload.Request.Actor.IsAgent() {
		return inst.Payload.Request.Actor.String() + " asks"
	}
	var newest *job
	for _, j := range r.jobs {
		if j.State == JobRunning && j.ctx.Actor().IsAgent() && (newest == nil || j.Job > newest.Job) {
			newest = j
		}
	}
	if newest != nil {
		return newest.ctx.Actor().String() + " acting"
	}
	return ""
}
