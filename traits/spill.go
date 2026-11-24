package traits

import "ofam.dev/ochazuke/core"

// SpillConfig declares which viewport edges spill directional motion commands
// into directional focus commands.
type SpillConfig struct {
	Top    bool `json:"top,omitempty"`
	Bottom bool `json:"bottom,omitempty"`
	Left   bool `json:"left,omitempty"`
	Right  bool `json:"right,omitempty"`
}

func factBool(facts map[string]any, key string) bool {
	if facts == nil {
		return false
	}
	b, _ := facts[key].(bool)
	return b
}

// ResolveSpill translates a directional motion command at a viewport boundary
// into the corresponding focus command when cfg enables spilling on that edge.
// It preserves Count, Source, and Actor on the returned command.
func ResolveSpill(facts map[string]any, cfg SpillConfig, cmd core.Command) (next core.Command, spilled bool) {
	var target string
	switch cmd.ID {
	case "motion.up":
		if cfg.Top && factBool(facts, "atTop") {
			target = core.CmdFocusUp
		}
	case "motion.down":
		if cfg.Bottom && factBool(facts, "atBottom") {
			target = core.CmdFocusDown
		}
	case "motion.left":
		if cfg.Left && factBool(facts, "atLeft") {
			target = core.CmdFocusLeft
		}
	case "motion.right":
		if cfg.Right && factBool(facts, "atRight") {
			target = core.CmdFocusRight
		}
	}
	if target == "" {
		return cmd, false
	}
	return core.Command{
		ID:     target,
		Count:  cmd.Count,
		Source: cmd.Source,
		Actor:  cmd.Actor,
	}, true
}
