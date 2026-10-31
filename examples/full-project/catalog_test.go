package main

import (
	"bytes"
	"testing"

	"ofam.dev/ochazuke/cli"
)

// The catalog is a pure function of the registered runtime: twenty fresh
// builds of the configured project answer the same bytes, so no map order
// reaches the output.
func TestCatalogIsDeterministic(t *testing.T) {
	var first []byte
	for i := range 20 {
		cfg, err := LoadConfig("")
		if err != nil {
			t.Fatal(err)
		}
		m, err := Build(cfg)
		if err != nil {
			t.Fatal(err)
		}
		data, err := cli.Catalog(m.Runtime, "full-project", "test")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = data
			continue
		}
		if !bytes.Equal(first, data) {
			t.Fatalf("catalog run %d differs from run 0", i)
		}
	}
}
