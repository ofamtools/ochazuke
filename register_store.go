package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RegisterPersistSetting keeps explicitly named text registers across processes.
// Special registers and recorded macros remain in memory only.
const RegisterPersistSetting = "register.persist"

func (r *Runtime) registerDirectory(key string) string {
	if len(key) != 1 || key[0] < 'a' || key[0] > 'z' || !r.Settings().Bool(RegisterPersistSetting) {
		return ""
	}
	dir := r.Config().StateDirectory()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "registers")
}

// namedRegister refreshes only a replaced file. Local macro recordings and
// other in-memory edits survive reads of the unchanged on-disk version.
func (r *Runtime) namedRegister(key string) (Register, error) {
	g := r.Registers()
	dir := r.registerDirectory(key)
	if dir == "" {
		return g.Named[key], nil
	}
	path := filepath.Join(dir, key+".json")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return g.Named[key], nil
	}
	if err != nil {
		return Register{}, fmt.Errorf("register %s: %w", key, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Register{}, fmt.Errorf("register %s: %w", key, err)
	}
	if previous := g.files[path]; previous != nil && os.SameFile(previous, info) && previous.ModTime() == info.ModTime() && previous.Size() == info.Size() {
		return g.Named[key], nil
	}
	var y Yank
	if err := json.NewDecoder(f).Decode(&y); err != nil {
		return Register{}, fmt.Errorf("register %s: %w", key, err)
	}
	if g.files == nil {
		g.files = map[string]os.FileInfo{}
	}
	g.files[path] = info
	g.Named[key] = Register{Yank: y}
	return g.Named[key], nil
}

// persistRegister commits one named yank before publishing it in memory.
// Atomic replacement makes concurrent processes last-writer-wins without a
// shared read/modify/write snapshot that could replace unrelated registers.
func (r *Runtime) persistRegister(key string, y Yank) error {
	dir := r.registerDirectory(key)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("register %s: %w", key, err)
	}
	f, err := os.CreateTemp(dir, ".register-*")
	if err != nil {
		return fmt.Errorf("register %s: %w", key, err)
	}
	defer os.Remove(f.Name())
	encodeErr := json.NewEncoder(f).Encode(y)
	info, statErr := f.Stat()
	closeErr := f.Close()
	if err := errors.Join(encodeErr, statErr, closeErr); err != nil {
		return fmt.Errorf("register %s: %w", key, err)
	}
	path := filepath.Join(dir, key+".json")
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("register %s: %w", key, err)
	}
	g := r.Registers()
	if g.files == nil {
		g.files = map[string]os.FileInfo{}
	}
	g.files[path] = info
	return nil
}
