package ochazuke

import (
	"bytes"
	"encoding/json"
	"strings"
)

func (s *Settings) protected(one Setting) bool {
	return one.Protected && s.actor != nil && s.actor().IsAgent()
}

func (s *Settings) checkProtected(one Setting) error {
	if !s.protected(one) {
		return nil
	}
	verb := "ochazuke.setting"
	if s.source != nil {
		if _, via := s.source(); via != "" {
			verb = via
		}
	}
	return &RefusedError{Verb: verb, Rule: "protected setting", Reason: one.Name + " may only be changed by the owner"}
}

// The file writer also guards callers restoring whole snapshots and old names.
func (s *Settings) checkProtectedFile(raw map[string]json.RawMessage) error {
	if s.actor == nil || !s.actor().IsAgent() {
		return nil
	}
	for _, one := range s.declared {
		if !s.protected(one) {
			continue
		}
		for _, name := range append([]string{one.Name}, one.Deprecated...) {
			if !bytes.Equal(raw[name], s.raw[name]) {
				return s.checkProtected(one)
			}
		}
	}
	return nil
}

// Bulk operations keep protected values and report the names they skipped.
func (s *Settings) writableNames(names []string) []string {
	if s.actor == nil || !s.actor().IsAgent() {
		return names
	}
	var allowed, skipped []string
	for _, name := range names {
		if one := s.find(name); one != nil && s.protected(*one) {
			skipped = append(skipped, name)
		} else {
			allowed = append(allowed, name)
		}
	}
	if len(skipped) > 0 && s.info != nil {
		s.info("Skipped protected settings: " + strings.Join(skipped, ", "))
	}
	return allowed
}
