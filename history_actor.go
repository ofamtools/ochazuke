package ochazuke

// historyByActor applies actor matching before the result limit.
func (r *Runtime) historyByActor(limit int, kinds []string, filter string) ([]HistoryEntry, error) {
	var match func(Actor) bool
	switch filter {
	case "", "all":
		match = func(Actor) bool { return true }
	case "self":
		me := r.Actor()
		match = func(a Actor) bool {
			if me.IsAgent() {
				return me.SameAgent(a)
			}
			return a.normalized() == me
		}
	case ActorOwner, ActorAgent, ActorUnknown:
		match = func(a Actor) bool { return a.normalized().Kind == filter }
	default:
		actor, err := ParseActor(filter)
		if err != nil {
			return nil, err
		}
		match = func(a Actor) bool { return a.normalized() == actor }
	}
	out := []HistoryEntry{}
	for _, e := range r.HistoryOf(0, kinds...) {
		if match(e.Context.Actor) {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
