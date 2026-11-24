package traits

// Standard dismiss step names evaluated in order by ResolveDismiss.
const (
	DismissStopEdit     = "stop_edit"
	DismissStopFilter   = "stop_filter"
	DismissClearQuery   = "clear_query"
	DismissClearVisual  = "clear_visual"
	DismissFocusPrimary = "focus_primary"
	DismissClose        = "close"
)

// DismissConfig lists dismiss steps in priority order.
type DismissConfig struct {
	Steps []string `json:"steps,omitempty"`
}

// DismissVerbs maps each dismiss step to the atomic verb ID to execute.
// Empty fields fall back to Ochazuke's default verb IDs.
type DismissVerbs struct {
	StopEdit     string
	StopFilter   string
	ClearQuery   string
	ClearVisual  string
	FocusPrimary string
	Close        string
}

func verbOr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func hasNonEmptyQuery(facts map[string]any) bool {
	if facts == nil {
		return false
	}
	if empty, ok := facts["queryEmpty"].(bool); ok {
		return !empty
	}
	if has, ok := facts["hasFilter"].(bool); ok {
		return has
	}
	if q, ok := facts["query"].(string); ok {
		return q != ""
	}
	return false
}

// ResolveDismiss walks cfg.Steps in order and returns the verb ID of the first
// step whose corresponding condition holds in facts.
func ResolveDismiss(facts map[string]any, cfg DismissConfig, verbs DismissVerbs) (cmdID string, handled bool) {
	for _, step := range cfg.Steps {
		switch step {
		case DismissStopEdit:
			if factBool(facts, "editing") {
				return verbOr(verbs.StopEdit, "form.stop"), true
			}
		case DismissStopFilter:
			if factBool(facts, "filtering") {
				return verbOr(verbs.StopFilter, "picker.filter.stop"), true
			}
		case DismissClearQuery:
			if hasNonEmptyQuery(facts) {
				return verbOr(verbs.ClearQuery, "picker.filter.clear"), true
			}
		case DismissClearVisual:
			if factBool(facts, "visualActive") {
				return verbOr(verbs.ClearVisual, "visual.clear"), true
			}
		case DismissFocusPrimary:
			if factBool(facts, "focusSecondary") {
				return verbOr(verbs.FocusPrimary, "focus.primary"), true
			}
		case DismissClose:
			return verbOr(verbs.Close, "modal.cancel"), true
		}
	}
	return "", false
}
