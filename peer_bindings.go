package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// PeerNoticesSetting controls missing-peer notices: user (default), all, off.
const PeerNoticesSetting = "bindings.peer-notices"

// BindingPeer describes a registered extension for binding diagnostics. Nouns
// are its mounted host names; Namespace is the stable binary namespace.
type BindingPeer struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Version   string   `json:"version,omitempty"`
	Nouns     []string `json:"nouns"`
}

// PeerBinding is a host-resolved catalog row. Problem carries a host-side
// rejection (namespace mapping, reserved keys or policy); it installs no row.
type PeerBinding struct {
	Extension string  `json:"extension"`
	Row       Binding `json:"row"`
	Problem   string  `json:"problem,omitempty"`
}

// PeerBindingStatus explains every declared peer row, including dropped rows.
type PeerBindingStatus struct {
	PeerBinding
	Loaded bool   `json:"loaded"`
	Reason string `json:"reason,omitempty"`
}

// ReloadPeerBindings replaces the whole soft layer after a host mount/sync.
// User rows are lifted before it and loaded last, so owner overrides and unbinds
// always win. Call even with no rows after removing the final extension.
func (r *Runtime) ReloadPeerBindings(rows []PeerBinding, peers []BindingPeer) []PeerBindingStatus {
	c := r.Config()
	r.unapplyUserBindings()
	r.unapplyBindingRows(c.peerApplied)
	c.peerApplied = nil
	c.peers = slices.Clone(peers)
	for i := range c.peers {
		c.peers[i].Nouns = slices.Clone(c.peers[i].Nouns)
	}
	c.peerRows = slices.Clone(rows)
	c.peerStatuses = nil
	batch := rowBatch{}
	for _, decl := range c.peerRows {
		status := PeerBindingStatus{PeerBinding: decl}
		row := decl.Row
		reason := decl.Problem
		if reason == "" && row.Instance != "" {
			reason = "peer rows cannot name an instance"
		}
		if reason == "" && (row.Unbind || row.Request.Empty()) {
			reason = "peer rows cannot unbind keys"
		}
		if reason == "" {
			normalized, err := r.validateRow(row)
			if err != nil {
				reason = r.peerBindingReason(row, err)
			} else {
				// Soft rows never replace an existing host/default row, even one with
				// a different guard. A batch may still contain available alternatives.
				for _, existing := range r.tableRows(normalized) {
					if existing.sameKeys(normalized) && !batch[sourceKey(normalized)] && !isPrefixCancel(existing) {
						reason = "key is already bound"
						break
					}
				}
				if reason == "" {
					_, err = r.applyBindingRow(row, batch, &c.peerApplied)
					if err != nil {
						reason = r.peerBindingReason(row, err)
					}
				}
			}
		}
		status.Loaded = reason == ""
		status.Reason = reason
		c.peerStatuses = append(c.peerStatuses, status)
	}
	r.ensurePeerNotices()
	r.ReloadUserBindings()
	r.syncPeerNotices()
	return r.PeerBindings()
}

// PeerBindings reports the last peer-layer load, for a host's inspector/detail.
func (r *Runtime) PeerBindings() []PeerBindingStatus {
	return slices.Clone(r.Config().peerStatuses)
}

func (r *Runtime) peerWarnings() []string {
	var out []string
	for _, status := range r.Config().peerStatuses {
		if !status.Loaded {
			row := status.Row
			out = append(out, fmt.Sprintf("warning: %s peer row %s on %s: %s: %s", status.Extension, row.Keys+row.Pattern, row.Noun, row.Request.Key(), status.Reason))
		}
	}
	return out
}

func (r *Runtime) bindingPeer(noun string) (BindingPeer, bool) {
	for _, peer := range r.Config().peers {
		if slices.Contains(peer.Nouns, noun) || peer.Namespace != "" && strings.HasPrefix(noun, peer.Namespace+".") {
			return peer, true
		}
	}
	return BindingPeer{}, false
}

// peerBindingReason uses presence, not version comparisons. Versions only
// explain an installed peer that lacks the verb/argument the row needs.
func (r *Runtime) peerBindingReason(row Binding, fallback error) string {
	noun := r.CanonicalNoun(row.Request.Noun)
	peer, known := r.bindingPeer(noun)
	if r.Noun(noun) == nil {
		if ns, _, dotted := strings.Cut(noun, "."); dotted && ns != FrameworkNamespace && !known {
			return fmt.Sprintf("extension %s is not registered; the key loads when it is", ns)
		}
		return "no noun " + noun
	}
	if r.Noun(row.Noun) == nil && row.Instance == "" {
		return "no noun " + row.Noun
	}
	label := strings.TrimSpace(peer.Name + " " + peer.Version)
	op, found := r.Operation(row.Request.Key())
	if !row.Unbind && !row.Request.Empty() && !found && known {
		return fmt.Sprintf("%s has no verb %s", label, row.Request.Key())
	}
	if found && known {
		for arg := range row.From {
			if _, ok := op.Parameters[arg]; !ok && !(arg == "target" && op.CatalogProxy) {
				return fmt.Sprintf("%s has no argument %s (%s)", op.Key(), arg, label)
			}
		}
	}
	return fallback.Error()
}

func (r *Runtime) userRowProblem(where string, row Binding, err error) string {
	// Missing dotted peers get a retryable warning rather than a typo-shaped
	// fatal notice. Ordinary malformed rows retain the existing diagnostics.
	noun := r.CanonicalNoun(row.Request.Noun)
	_, known := r.bindingPeer(noun)
	if (strings.Contains(noun, ".") && !strings.HasPrefix(noun, FrameworkNamespace+".") && r.Noun(noun) == nil) || known {
		return fmt.Sprintf("warning: %s: %s %s: %s: %s", BindingsFile, where, row.Keys+row.Pattern, row.Request.Key(), r.peerBindingReason(row, err))
	}
	return rowProblem(where, row, err)
}

func (r *Runtime) ensurePeerNotices() {
	c := r.Config()
	if c.peerWatching {
		return
	}
	c.peerWatching = true
	r.Settings().Observe(PeerNoticesSetting, func(string, any) { r.syncPeerNotices() })
}

func (r *Runtime) syncPeerNotices() {
	c := r.Config()
	if c.Withdraw != nil {
		for _, line := range c.peerNotices {
			c.Withdraw(line)
		}
	}
	c.peerNotices = nil
	mode := r.Settings().String(PeerNoticesSetting)
	if mode == "all" {
		c.peerNotices = append(c.peerNotices, r.peerWarnings()...)
	}
	if mode != "off" {
		for _, line := range c.problems {
			if strings.HasPrefix(line, "warning: "+BindingsFile+":") {
				c.peerNotices = append(c.peerNotices, line)
			}
		}
	}
	for _, line := range c.peerNotices {
		c.report(line)
	}
}
