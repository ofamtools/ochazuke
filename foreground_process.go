package ochazuke

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

// Retain only the tail, not arbitrary foreground output.
type stderrTail struct{ data []byte }

func (s *stderrTail) Write(p []byte) (int, error) {
	const limit = 4096
	n := len(p)
	if len(p) >= limit {
		s.data = append(s.data[:0], p[len(p)-limit:]...)
		return n, nil
	}
	if extra := len(s.data) + len(p) - limit; extra > 0 {
		copy(s.data, s.data[extra:])
		s.data = s.data[:len(s.data)-extra]
	}
	s.data = append(s.data, p...)
	return n, nil
}
func (s *stderrTail) line() string {
	if s == nil {
		return ""
	}
	text := strings.TrimSpace(string(s.data))
	if at := strings.LastIndexAny(text, "\r\n"); at >= 0 {
		text = text[at+1:]
	}
	return text
}

type foregroundProcess struct{ msg ForegroundMsg }

func (p *foregroundProcess) SetStdin(r io.Reader) {
	if p.msg.Cmd.Stdin == nil {
		p.msg.Cmd.Stdin = r
	}
}
func (p *foregroundProcess) SetStdout(w io.Writer) {
	if p.msg.Cmd.Stdout == nil {
		p.msg.Cmd.Stdout = w
	}
}
func (p *foregroundProcess) SetStderr(w io.Writer) {
	if p.msg.Cmd.Stderr == nil {
		p.msg.Cmd.Stderr = w
	}
}
func (p *foregroundProcess) Run() error {
	cmd := p.msg.Cmd
	if p.msg.stderr == nil {
		p.msg.stderr = &stderrTail{}
	}
	finish, err := foregroundStderr(cmd, p.msg.stderr)
	if err != nil {
		return err
	}
	err = cmd.Run()
	finish()
	if err != nil && cmd.ProcessState != nil && p.msg.hold {
		if in, ok := cmd.Stdin.(*os.File); ok && term.IsTerminal(in.Fd()) {
			_, _ = io.WriteString(cmd.Stdout, "\nPress any key to return…")
			state, rawErr := term.MakeRaw(in.Fd())
			if rawErr == nil {
				var key [1]byte
				_, _ = in.Read(key[:])
				_ = term.Restore(in.Fd(), state)
			}
		}
	}
	return err
}
