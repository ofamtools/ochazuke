// Atomic replacement and first-original backups adapt matomeru/internal/setup
// (MIT, Copyright Joshua Toyota). Staging and rollback extend it to a batch.
package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type staged struct {
	write  Write
	temp   string
	undo   string
	backup string
}

// Apply preflights the whole plan and stages every file before running harness
// commands or replacing targets. A staging failure changes no target; a rename
// failure rolls back files already replaced. External harnesses are independent
// transactions: a successful harness command cannot be rolled back safely when
// a later command fails. The error names that limitation and completed items.
func (s *Setup) Apply(ctx context.Context, p *Plan) error {
	if p == nil {
		return fmt.Errorf("nil setup plan")
	}
	for _, st := range p.Items {
		if p.excluded[st.ID] {
			continue
		}
		if st.State == Conflict || st.State == External {
			return fmt.Errorf("%s: %s: %s", st.ID, st.State, st.Reason)
		}
	}
	for _, cmd := range p.Commands {
		if len(cmd.Argv) == 0 {
			return fmt.Errorf("%s: empty argv", cmd.ID)
		}
		if _, err := s.lookPath(cmd.Argv[0]); err != nil {
			return fmt.Errorf("%s: %w", cmd.ID, err)
		}
		if err := s.unchangedCommand(ctx, p, cmd.ID); err != nil {
			return err
		}
	}
	stages := []staged{}
	defer func() {
		for _, st := range stages {
			for _, path := range []string{st.temp, st.undo, st.backup} {
				if path != "" {
					_ = os.Remove(path)
				}
			}
		}
	}()
	for _, w := range p.Writes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := unchanged(w); err != nil {
			return err
		}
		backup := w.Path + "." + s.options.Tool + ".bak"
		if err := regularPath(backup); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(w.Path), 0700); err != nil {
			return err
		}
		if err := regularPath(w.Path); err != nil {
			return err
		}
		st := staged{write: w}
		stages = append(stages, st)
		at := len(stages) - 1
		var err error
		if !w.Remove {
			stages[at].temp, err = stage(w.Path, []byte(w.Content), mode(w.before))
			if err != nil {
				return err
			}
		}
		if w.before.exists {
			stages[at].undo, err = stage(w.Path, w.before.data, w.before.mode)
			if err != nil {
				return err
			}
			if _, err = os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
				stages[at].backup, err = stage(w.Path, w.before.data, w.before.mode)
				if err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
	}
	// Recheck after staging. Plan is also safe to review before calling Apply.
	for _, st := range stages {
		if err := unchanged(st.write); err != nil {
			return err
		}
	}
	completed := []string{}
	for _, cmd := range p.Commands {
		if err := s.unchangedCommand(ctx, p, cmd.ID); err != nil {
			if len(completed) > 0 {
				return fmt.Errorf("%w (harness changes already completed for %v; files unchanged)", err, completed)
			}
			return err
		}
		if _, err := s.execute(ctx, cmd.Argv); err != nil {
			if len(completed) > 0 {
				return fmt.Errorf("%s: %w (harness changes already completed for %v; files unchanged)", cmd.ID, err, completed)
			}
			return fmt.Errorf("%s: %w (files unchanged)", cmd.ID, err)
		}
		completed = append(completed, cmd.ID)
	}
	for n, st := range stages {
		err := ctx.Err()
		if err == nil {
			err = unchanged(st.write)
		}
		if err == nil && st.backup != "" {
			err = os.Link(st.backup, st.write.Path+"."+s.options.Tool+".bak")
			if errors.Is(err, os.ErrExist) {
				err = nil
			}
		}
		if err == nil {
			if st.write.Remove {
				err = os.Remove(st.write.Path)
				if errors.Is(err, os.ErrNotExist) {
					err = nil
				}
			} else {
				err = os.Rename(st.temp, st.write.Path)
			}
		}
		if err != nil {
			for j := n - 1; j >= 0; j-- {
				prev := stages[j]
				if prev.write.before.exists {
					err = errors.Join(err, os.Rename(prev.undo, prev.write.Path))
				} else {
					e := os.Remove(prev.write.Path)
					if !errors.Is(e, os.ErrNotExist) {
						err = errors.Join(err, e)
					}
				}
			}
			if len(completed) > 0 {
				return fmt.Errorf("file transaction failed (harness changes completed for %v): %w", completed, err)
			}
			return err
		}
	}
	return nil
}

func (s *Setup) unchangedCommand(ctx context.Context, p *Plan, id string) error {
	for _, item := range p.Items {
		if item.ID != id {
			continue
		}
		state, _, err := s.commandStatus(ctx, item.Item)
		if err != nil {
			return err
		}
		if state != item.State {
			return fmt.Errorf("%s changed since setup plan (%s to %s); plan again", id, item.State, state)
		}
		return nil
	}
	return fmt.Errorf("%s has no setup item in the plan", id)
}

func mode(s snapshot) os.FileMode {
	if s.exists {
		return s.mode
	}
	return 0600
}
func unchanged(w Write) error {
	have, err := read(w.Path)
	if err != nil {
		return err
	}
	if have.exists != w.before.exists || have.mode != w.before.mode || !bytes.Equal(have.data, w.before.data) {
		return fmt.Errorf("%s changed since setup plan; plan again", w.Path)
	}
	return nil
}
func stage(path string, data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".setup-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return "", err
	}
	if err = f.Chmod(mode); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
