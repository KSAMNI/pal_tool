//go:build !windows

package app

import "os"

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
