package cli

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke"
)

// A catalog's declared namespace wins over its bin name; without one the
// bin name less o- is the namespace.
func TestCatalogNamespaceDeclaredOverBin(t *testing.T) {
	if ns, err := CatalogNamespace(CatalogDoc{Namespace: "question"}, "o-questions"); err != nil || ns != "question" {
		t.Fatalf("declared: %q %v", ns, err)
	}
	if ns, err := CatalogNamespace(CatalogDoc{}, "/usr/bin/o-questions"); err != nil || ns != "questions" {
		t.Fatalf("undeclared: %q %v", ns, err)
	}
	if _, err := CatalogNamespace(CatalogDoc{Namespace: ochazuke.FrameworkNamespace}, "o-x"); err == nil {
		t.Fatal("a declared reserved namespace was accepted")
	}
	was, declared := ochazuke.DefaultNamespace, ochazuke.DeclaredNamespace
	t.Cleanup(func() { ochazuke.DefaultNamespace, ochazuke.DeclaredNamespace = was, declared })
	if err := ochazuke.DeclareNamespace("cmd"); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(ochazuke.NewRuntime(), "o-cmds", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil || doc.Namespace != "cmd" {
		t.Fatalf("the catalog carries the declared namespace: %q %v", doc.Namespace, err)
	}
	if ns, err := OwnNamespace("o-cmds"); err != nil || ns != "cmd" {
		t.Fatalf("own: %q %v", ns, err)
	}
}
