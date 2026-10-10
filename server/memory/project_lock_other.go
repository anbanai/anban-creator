//go:build !unix

package memory

func lockProject(string) (func(), error) {
	// FilesystemStore's process mutex is the strongest portable primitive here.
	return func() {}, nil
}
