package mcp

import "ofam.dev/ochazuke"

// statuses are an MCP answer's status of each outcome: the first row whose
// kind matches and whose cause is "" or the outcome's. Nothing and choose
// are results (isError false); refused and failed are errors.
var statuses = []struct {
	kind   ochazuke.OutcomeKind
	cause  ochazuke.OutcomeCause
	status string
}{
	{ochazuke.OutcomeNeeds, ochazuke.CauseConfirm, "refused"},
	{ochazuke.OutcomeFailed, ochazuke.CauseRights, "refused"},
	{ochazuke.OutcomeDone, "", "done"},
	{ochazuke.OutcomeNothing, "", "nothing"},
	{ochazuke.OutcomeNeeds, "", "choose"},
	{ochazuke.OutcomeFailed, "", "failed"},
}

// Answer is MCP's encoder of an outcome: it writes o into answer, the
// status (statuses; needs answered as one only when needs is true, else
// failed, as cli.needs-status error says) and, for any outcome but done,
// its kind (outcome), verb, cause and reason in the one wording
// (Outcome.Text); for needs what it needs (needs: param, noun, given and
// the choices) and the choices with their commands (choices), and how many
// candidates were not listed (more, cli.choices-max); the command the owner
// or the caller runs (command) when it names one.
func Answer(answer map[string]any, o ochazuke.Outcome, needs bool) {
	status := "failed"
	for _, row := range statuses {
		if row.kind == o.Kind && (row.cause == "" || row.cause == o.Cause) {
			status = row.status
			break
		}
	}
	if status == "choose" && !needs {
		status = "failed"
	}
	answer["status"] = status
	if o.Kind == ochazuke.OutcomeDone {
		return
	}
	answer["outcome"], answer["verb"], answer["cause"], answer["reason"] = o.Kind, o.Verb, o.Cause, o.Text()
	if o.Needs != nil {
		answer["needs"], answer["choices"] = o.Needs, o.Needs.Choices
		if o.Needs.More > 0 {
			answer["more"] = o.Needs.More
		}
	}
	if o.Command != "" {
		answer["command"] = o.Command
	}
}

// outcomeResult is a tool call's answer: an error that is a nothing or a
// needs outcome (a ChoiceAnswer, a name no thing has) is a result with
// its status (Answer), not an error; any other error stays one.
func outcomeResult(result any, err error) (any, error) {
	if err == nil {
		return result, nil
	}
	answer := map[string]any{}
	Answer(answer, ochazuke.OutcomeOf(nil, err), true)
	if status := answer["status"]; status == "failed" || status == "refused" {
		return nil, err
	}
	return answer, nil
}
