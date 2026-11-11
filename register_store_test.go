package ochazuke

import (
	"os"
	"path/filepath"
	"testing"
)

func registerRuntime(dir string) *Runtime {
	r := NewRuntime()
	r.Config().Disable()
	r.Config().StateDir = dir
	return r
}

func setRegisterText(t *testing.T, r *Runtime, name, text string) {
	t.Helper()
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Args: Arguments{"name": name, "props": map[string]any{"text": text, "shape": "char"}}}); err != nil {
		t.Fatal(err)
	}
}

func requireRegisterText(t *testing.T, r *Runtime, name, want string) {
	t.Helper()
	value, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "get", Args: Arguments{"name": name, "prop": "text"}})
	if err != nil || value != want {
		t.Fatalf("register %s: %#v %v, want %q", name, value, err, want)
	}
	if y, err := r.ReadYank(name); err != nil || y.Text != want {
		t.Fatalf("paste register %s: %+v %v, want %q", name, y, err, want)
	}
}

func TestNamedRegistersRetainAndRefreshAcrossRuntimes(t *testing.T) {
	dir := t.TempDir()
	host, callback := registerRuntime(dir), registerRuntime(dir)
	setRegisterText(t, host, "a", "before")
	setRegisterText(t, host, "b", "unrelated")
	requireRegisterText(t, callback, "A", "before")
	setRegisterText(t, callback, "a", "callback output\n")
	requireRegisterText(t, host, "a", "callback output\n")
	setRegisterText(t, host, "a", "new host value")
	requireRegisterText(t, host, "a", "new host value")
	if _, err := callback.Execute(Request{Noun: "ochazuke.register", Verb: "append", Args: Arguments{"name": "a", "text": " plus errors", "shape": "char"}}); err != nil {
		t.Fatal(err)
	}
	requireRegisterText(t, host, "a", "new host value plus errors")
	restarted := registerRuntime(dir)
	requireRegisterText(t, restarted, "a", "new host value plus errors")
	requireRegisterText(t, restarted, "b", "unrelated")
	value, err := restarted.Execute(Request{Noun: "ochazuke.register", Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	answer, ok := value.(ListAnswer)
	if !ok || len(answer.Items) != 2 {
		t.Fatalf("retained registers list: %#v", value)
	}
	for _, name := range []string{"a", "b"} {
		info, err := os.Stat(filepath.Join(dir, "registers", name+".json"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private register %s: %v %v", name, info, err)
		}
	}
}

func TestNamedRegisterPersistenceOptOut(t *testing.T) {
	dir := t.TempDir()
	r := registerRuntime(dir)
	setRegisterText(t, r, "a", "retained")
	if err := r.Settings().Set(RegisterPersistSetting, false); err != nil {
		t.Fatal(err)
	}
	setRegisterText(t, r, "a", "memory only")
	requireRegisterText(t, r, "a", "memory only")
	requireRegisterText(t, registerRuntime(dir), "a", "retained")
	if err := r.Settings().Set(RegisterPersistSetting, true); err != nil {
		t.Fatal(err)
	}
	// Re-enabling does not overwrite a newer in-memory value with the
	// unchanged durable version that runtime itself wrote earlier.
	requireRegisterText(t, r, "a", "memory only")
	setRegisterText(t, registerRuntime(dir), "a", "new external value")
	requireRegisterText(t, r, "a", "new external value")
}

func TestNamedRegisterPersistenceFailureIsNotSuccess(t *testing.T) {
	dir := t.TempDir()
	r := registerRuntime(dir)
	setRegisterText(t, r, "a", "before")
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Config().StateDir = blocked
	if err := r.StoreYank(Yank{Text: "lost", Shape: "char"}, "a", false); err == nil {
		t.Fatal("a failed durable write reported success")
	}
	if got := r.Registers().Named["a"].Yank.Text; got != "before" {
		t.Fatalf("failed write replaced memory with %q", got)
	}
	r.Config().StateDir = dir
	if err := os.WriteFile(filepath.Join(dir, "registers", "a.json"), []byte("broken JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadYank("a"); err == nil {
		t.Fatal("a malformed durable value was silently ignored")
	}
}

func TestNamedRegisterPersistenceExcludesMacrosAndSpecialRegisters(t *testing.T) {
	dir := t.TempDir()
	r := registerRuntime(dir)
	setRegisterText(t, r, "a", "retained text")
	for _, request := range []Request{
		{Noun: "ochazuke.macro", Verb: "start", Args: Arguments{"register": "a"}},
		{Noun: "ochazuke.macro", Verb: "append", Args: Arguments{"key": "j"}},
		{Noun: "ochazuke.macro", Verb: "stop"},
	} {
		if _, err := r.Execute(request); err != nil {
			t.Fatal(err)
		}
	}
	requireRegisterText(t, r, "a", "j")
	requireRegisterText(t, r, "a", "j")
	requireRegisterText(t, registerRuntime(dir), "a", "retained text")
	for _, name := range []string{"\"", "0", "1", "-", "+", "*", "_"} {
		if err := r.StoreYank(Yank{Text: "private transient", Shape: "char"}, name, false); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(filepath.Join(dir, "registers"))
	if err != nil || len(files) != 1 || files[0].Name() != "a.json" {
		t.Fatalf("special register persisted: %v %v", files, err)
	}
	memory := NewRuntime()
	memory.Config().Disable()
	if err := memory.StoreYank(Yank{Text: "sandbox", Shape: "char"}, "a", false); err != nil {
		t.Fatal(err)
	}
	requireRegisterText(t, memory, "a", "sandbox")
}
