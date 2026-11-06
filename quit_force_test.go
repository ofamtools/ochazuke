package ochazuke

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/keytest"
)

func TestQuitForceAfterProtectsPendingWrite(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"double-tap", "deliberate", "zero", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			r, s := editFixture(t)
			op, _ := r.Operation("parcel.set")
			r.Register(Operation{Noun: "parcel", Verb: "set", Parameters: op.Parameters, App: func(c Ctx) Result {
				changes := maps.Clone(c.Args["props"].(map[string]any))
				return Pending(func(ctx context.Context) (any, error) {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					maps.Copy(s.find("t1"), changes)
					return nil, nil
				})
			}})
			m := NewModel(r)
			mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
			e := r.RecordEditor(editFormID)
			for _, key := range []string{"A", "x", "esc", "esc"} {
				if _, err := r.HandleBinding(key); err != nil {
					t.Fatal(err)
				}
				e.PendingCommand()
			}
			pending := r.PendingCommands()
			if mode == "zero" {
				if _, err := r.Settings().SetSession("quit.force-after", "0s"); err != nil {
					t.Fatal(err)
				}
			}
			keytest.Press(m, "ctrl+c")
			if mode == "deliberate" {
				r.eventHub().quitRequested = time.Now().Add(-time.Second)
			}
			if mode == "explicit" {
				mustRun(t, r, Request{Noun: "app", Verb: "quit", Args: Arguments{"force": true}})
				m.quitHooks()
			} else {
				keytest.Press(m, "ctrl+c")
			}
			deliverEditCommand(r, pending)
			if mode == "double-tap" {
				if got := s.find("t1")["title"]; got != "Originalx" {
					t.Fatalf("double tap lost healthy write: %v", got)
				}
				if len(m.Notices) != 0 {
					t.Fatalf("healthy write reported unsaved: %v", m.Notices)
				}
			} else {
				if got := s.find("t1")["title"]; got != "Original" {
					t.Fatalf("forced quit ran cancelled write: %v", got)
				}
				if !strings.Contains(strings.Join(m.Notices, "\n"), `title = "Originalx"`) {
					t.Fatalf("forced quit lost attempted value: %v", m.Notices)
				}
			}
		})
	}
}
