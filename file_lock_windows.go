//go:build windows

package ochazuke

// lockFile is a no-op on Windows: the read-merge-write still narrows a
// lost update to the moment between the read and the rename.
func lockFile(string) (unlock func(), err error) { return func() {}, nil }
