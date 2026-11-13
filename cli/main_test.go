package cli

import (
	"os"
	"testing"
)

// TestMain clears the markers ochazuke.ActorFromEnv reads, so a suite run from
// inside an agent harness (Claude Code, Codex, Pi) is judged as the owner, as
// it is in CI. Tests that need an agent set the markers themselves. The rights
// gate is correct to refuse an agent; the tests must not inherit one.
func TestMain(m *testing.M) {
	for _, k := range []string{
		"OCHAZUKE_ACTOR", "OCHAZUKE_HARNESS", "OCHAZUKE_CALLER", "TUIOS_HARNESS", "AGENT_HARNESS",
		"CODEX_THREAD_ID", "CODEX_CI", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_ENTRYPOINT", "PI_SESSION_ID",
	} {
		os.Unsetenv(k)
	}
	code := m.Run()
	if serveToolDir != "" {
		os.RemoveAll(serveToolDir)
	}
	os.Exit(code)
}
