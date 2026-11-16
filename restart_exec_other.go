//go:build !unix

package ochazuke

// platformExec is nil where a process cannot replace itself: app.restart
// asks to quit and quits there.
var platformExec func(path string, argv, env []string) error
