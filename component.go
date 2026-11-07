package ochazuke

// componentAt returns the component of type T that the instance id fronts,
// or the zero value (nil for a pointer type) when there is none.
func componentAt[T any](r *Runtime, id string) (zero T) {
	if inst := r.Instances[id]; inst != nil {
		if c, ok := inst.Component.(T); ok {
			return c
		}
	}
	return zero
}

// nearestComponent returns the first component of type T on a focus's
// ancestry (the focus itself first), or the zero value when none is.
func nearestComponent[T comparable](r *Runtime, focus string) (zero T) {
	for _, id := range r.Ancestry(focus) {
		if c := componentAt[T](r, id); c != zero {
			return c
		}
	}
	return zero
}
