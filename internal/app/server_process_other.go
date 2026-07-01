//go:build !linux && !windows

package app

func detectProcessByExecutablePath(binary string) (bool, error) {
	return false, nil
}
