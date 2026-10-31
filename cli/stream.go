package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
)

// stream prints a watch's events as JSON Lines until ctx ends (an
// interrupt or SIGTERM ends it too, cleanly) or the watch does. It is the
// loop a TUI's program would be: each scheduled command runs on its own
// goroutine and its message is delivered here, on the shell's, so a job's
// completion, a store's pushed events and Runtime.Changed reach the
// watch as they happen. Nothing polls: with no command scheduled the
// shell only waits.
func (s *Shell) stream(ctx context.Context, w *ochazuke.Watch) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer w.Stop()
	r := s.Runtime
	msgs := make(chan tea.Msg)
	spawn := func(cmd tea.Cmd) {
		go func() {
			msg := cmd()
			select {
			case msgs <- msg:
			case <-ctx.Done():
			}
		}()
	}
	for {
		if err := s.printEvents(w.Take()); err != nil {
			return err
		}
		if w.Stopped() {
			return w.Err()
		}
		if s.Settle != nil {
			if err := s.Settle(); err != nil {
				fmt.Fprintln(s.stderr(), "error:", err)
			}
		}
		if cmd := r.PendingCommands(); cmd != nil {
			spawn(cmd)
		}
		select {
		case <-ctx.Done():
			return s.printEvents(w.Take())
		case msg := <-msgs:
			switch m := msg.(type) {
			case nil:
			case tea.BatchMsg:
				for _, c := range m {
					if c != nil {
						spawn(c)
					}
				}
			default:
				if _, err := r.Deliver(m); err != nil {
					fmt.Fprintln(s.stderr(), "error:", err)
				}
			}
		}
	}
}

// printEvents writes each event as one JSON line.
func (s *Shell) printEvents(events []ochazuke.WatchEvent) error {
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(s.stdout(), "%s\n", line); err != nil {
			return err
		}
	}
	return nil
}

// cacheVersions keeps a watch's versions in the shell's state cache
// (StateCache), one scope per watch query, so a later command's --since
// resumes from a hash an earlier one printed.
type cacheVersions struct{ s *Shell }

// watchList is the list name a watch's versions are filed under in its
// scope.
const watchList = "watch"

func (c cacheVersions) store(scope string) (stateStore, error) {
	dir, err := c.s.StateCache.dir(c.s.Bin)
	if err != nil {
		return stateStore{}, err
	}
	return stateStore{root: dir, scope: c.s.StateCache.scopeID(c.s.Bin, scope, nil), keep: c.s.StateCache.keep()}, nil
}

func (c cacheVersions) ReadVersion(scope, hash string) ([]string, []string, bool) {
	st, err := c.store(scope)
	if err != nil {
		return nil, nil, false
	}
	v, ok := st.read(watchList, hash)
	return v.ids, v.hashes, ok
}

func (c cacheVersions) WriteVersion(scope, hash string, ids, hashes []string) error {
	st, err := c.store(scope)
	if err != nil {
		return err
	}
	if err := st.write(watchList, hash, version{ids: ids, hashes: hashes}); err != nil {
		return err
	}
	return st.touch(c.s.StateCache.scopes())
}
