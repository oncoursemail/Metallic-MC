//go:build linux

package benchgate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type artifactRoot interface {
	Open(string) (*os.File, error)
	Close() error
}

type scopedRoot struct{ fd int }

func openArtifactRoot(path string) (artifactRoot, error) {
	// The explicitly supplied root is canonicalized once. Descendants are
	// subsequently opened relative to held directory descriptors, never via
	// process-global paths. O_NOFOLLOW on every step prevents symlink escapes,
	// including when intermediate directories are concurrently renamed.
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	realPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open(realPath, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return &scopedRoot{fd}, nil
}

func (r *scopedRoot) Close() error { return syscall.Close(r.fd) }

func (r *scopedRoot) Open(path string) (*os.File, error) {
	if !validPath(path) {
		return nil, errors.New("invalid relative trace path")
	}
	parts := strings.Split(path, "/")
	parent := r.fd
	owned := -1
	defer func() {
		if owned >= 0 {
			_ = syscall.Close(owned)
		}
	}()
	for _, part := range parts[:len(parts)-1] {
		next, err := syscall.Openat(parent, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		if owned >= 0 {
			_ = syscall.Close(owned)
		}
		parent, owned = next, next
	}
	// NONBLOCK prevents a malicious FIFO from blocking before f.Stat rejects it.
	fd, err := syscall.Openat(parent, parts[len(parts)-1], syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("trace is not a regular file")
	}
	return f, nil
}
