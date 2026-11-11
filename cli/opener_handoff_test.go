//go:build !windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

func TestOpenerHandoffProcess(t *testing.T) {
	if os.Getenv("OCHAZUKE_OPENER_HANDOFF_FIXTURE") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 || len(os.Args) != separator+3 {
		fmt.Fprintln(os.Stderr, "opener fixture needs its fake command and target")
		os.Exit(1)
	}
	// The explicit fake command is the only program launched; no installed
	// application or operating-system opener is called by this fixture.
	opener := ochazuke.Opener{GOOS: "linux"}
	if err := opener.OpenWith(os.Args[separator+1], os.Args[separator+2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(`"handed over"`)
	os.Exit(0)
}

func TestMountedOpenerSurvivesSuccessfulExtensionCleanup(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	observed := filepath.Join(dir, "opened")
	fake := filepath.Join(dir, "fake opener")
	target := filepath.Join(dir, "capture with spaces.txt")
	// The fake opener cannot publish its effect until the extension has
	// exited and RunMounted's group cleanup has completed. Bound its wait
	// so a failed test never leaves a long-lived descendant behind.
	script := `#!/bin/sh
attempt=0
while [ ! -f "$OPENER_RELEASE" ]; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 500 ] || exit 1
  sleep 0.01
done
printf 'opened\n%s\n%s\n' "$1" "$2" > "$OPENER_OBSERVED"
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(release, nil, 0600)
	env := append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+dir, "XDG_STATE_HOME="+dir, "XDG_DATA_HOME="+dir, "OCHAZUKE_OPENER_HANDOFF_FIXTURE=1", "OPENER_RELEASE="+release, "OPENER_OBSERVED="+observed)
	value, err := RunMounted(CatalogVerb{Name: "open"}, ProxyCommand{
		Bin: os.Args[0], Args: []string{"-test.run=^TestOpenerHandoffProcess$", "--", fake, target}, Env: env,
	}).Await(t.Context())
	if err != nil || value != "handed over" {
		t.Fatalf("extension handoff: %#v %v", value, err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	want := "opened\n--\n" + target + "\n"
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, readErr := os.ReadFile(observed)
		if readErr == nil && string(data) == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("opener did not survive extension cleanup: got %q %v, want %q", data, readErr, want)
		case <-ticker.C:
		}
	}
}
