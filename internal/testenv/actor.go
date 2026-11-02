// Package testenv isolates process environment used by framework test fixtures.
package testenv

import "testing"

// ClearActor removes detector signals and identity defaults for this test only.
// Each test must establish its own actor after calling it; cleanup restores the
// runner's environment, so agent detection remains enabled outside the fixture.
func ClearActor(t testing.TB) {
	t.Helper()
	for _, key := range []string{
		"OCHAZUKE_ACTOR", "OCHAZUKE_HARNESS", "TUIOS_HARNESS", "AGENT_HARNESS",
		"CODEX_THREAD_ID", "CODEX_CI", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID",
		"CLAUDE_CODE_ENTRYPOINT", "PI_SESSION_ID",
		"TUIOS_AGENT_NAME", "OCHAZUKE_MODEL", "AGENT_MODEL", "TUIOS_PANE_ID", "TUIOS_SESSION",
	} {
		t.Setenv(key, "")
	}
}
