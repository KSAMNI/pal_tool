package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

var atomicReplaceFile = replaceFile

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return atomicWriteFileFromReader(path, bytes.NewReader(data), perm)
}

func atomicWriteFileFromReader(path string, reader io.Reader, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, reader); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := atomicReplaceFile(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
