//go:build !linux && !darwin

package updater

func AcquireLock(_ string) (func(), error) {
	return func() {}, nil
}
