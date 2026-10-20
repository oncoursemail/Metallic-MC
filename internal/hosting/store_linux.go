//go:build linux

package hosting

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// All filesystem operations are relative to a held directory descriptor. A
// symlink swap cannot redirect a write or cleanup to another directory.
type fileStore struct {
	dir  *os.File
	lock *os.File
	base string
	// Narrow I/O seams let tests exercise failed writes and ambiguous commits
	// against real snapshots without depending on disk exhaustion or root mode.
	writeFile func(*os.File, []byte) (int, error)
	syncFile  func(*os.File) error
	closeFile func(*os.File) error
	rename    func(int, string, int, string) error
	syncDir   func(*os.File) error
}

func openDurableStore(path string) (durableStore, error) {
	dir, err := openPrivateDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	base := filepath.Base(path)
	fd, err := syscall.Openat(int(dir.Fd()), base+".lock", syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: opening hosting writer lock", ErrStore)
	}
	lock := os.NewFile(uintptr(fd), base+".lock")
	if err := checkPrivateFile(fd); err != nil {
		_ = lock.Close()
		_ = dir.Close()
		return nil, err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		_ = dir.Close()
		return nil, fmt.Errorf("%w: hosting store already in use or cannot be locked", ErrStore)
	}
	// Sync before exposing a recovered store, including a rename whose directory
	// sync may have failed in the previous process. Filesystems without directory
	// sync support do not qualify for this durability mode.
	if err := dir.Sync(); err != nil {
		_ = lock.Close()
		_ = dir.Close()
		return nil, fmt.Errorf("%w: syncing hosting directory", ErrStore)
	}
	return &fileStore{
		dir: dir, lock: lock, base: base,
		writeFile: func(f *os.File, b []byte) (int, error) { return f.Write(b) },
		syncFile:  func(f *os.File) error { return f.Sync() },
		closeFile: func(f *os.File) error { return f.Close() },
		rename:    syscall.Renameat,
		syncDir:   func(f *os.File) error { return f.Sync() },
	}, nil
}

func openPrivateDirectory(path string) (*os.File, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: opening hosting directory", ErrStore)
	}
	dir := os.NewFile(uintptr(fd), "/")
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		if err := checkDirectory(int(dir.Fd()), false); err != nil {
			_ = dir.Close()
			return nil, err
		}
		next, err := syscall.Openat(int(dir.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if errors.Is(err, syscall.ENOENT) {
			err = syscall.Mkdirat(int(dir.Fd()), part, 0700)
			if err == nil || errors.Is(err, syscall.EEXIST) {
				// Newly created directory entries also need their parent synced.
				if err = dir.Sync(); err == nil {
					next, err = syscall.Openat(int(dir.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
				}
			}
		}
		_ = dir.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: hosting path must use real local directories", ErrStore)
		}
		dir = os.NewFile(uintptr(next), part)
	}
	if err := checkDirectory(int(dir.Fd()), true); err != nil {
		_ = dir.Close()
		return nil, err
	}
	return dir, nil
}

func checkDirectory(fd int, private bool) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return fmt.Errorf("%w: invalid hosting directory", ErrStore)
	}
	uid := uint32(os.Geteuid())
	if private {
		if stat.Uid != uid || stat.Mode&07777 != 0700 {
			return fmt.Errorf("%w: hosting directory must be owned by this user with mode 0700", ErrStore)
		}
	} else if (stat.Uid != 0 && stat.Uid != uid) || (stat.Mode&0022 != 0 && stat.Mode&syscall.S_ISVTX == 0) {
		return fmt.Errorf("%w: hosting ancestor directory is writable by another user", ErrStore)
	}
	return nil
}

func checkPrivateFile(fd int) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Uid != uint32(os.Geteuid()) || stat.Mode&07777 != 0600 || stat.Nlink != 1 {
		return fmt.Errorf("%w: hosting files must be owned private regular files with mode 0600 and no hard links", ErrStore)
	}
	return nil
}

func (s *fileStore) read() ([]byte, error) {
	fd, err := syscall.Openat(int(s.dir.Fd()), s.base, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("%w: opening hosting state", ErrStore)
	}
	f := os.NewFile(uintptr(fd), s.base)
	defer f.Close()
	if err := checkPrivateFile(fd); err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || info.Size() > maxStoreBytes {
		return nil, fmt.Errorf("%w: hosting state exceeds storage bounds", ErrStore)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxStoreBytes+1))
	if err != nil || len(b) > maxStoreBytes {
		return nil, fmt.Errorf("%w: reading bounded hosting state", ErrStore)
	}
	return b, nil
}

func (s *fileStore) write(b []byte) error {
	if len(b) == 0 || len(b) > maxStoreBytes {
		return fmt.Errorf("%w: hosting state exceeds storage bounds", ErrStore)
	}
	dirFD := int(s.dir.Fd())
	if err := checkDirectory(dirFD, true); err != nil {
		return err
	}
	if err := checkPrivateFile(int(s.lock.Fd())); err != nil {
		return err
	}
	// Reject a replaced target rather than silently overwriting a symlink,
	// non-private file, directory, device or FIFO.
	current, err := syscall.Openat(dirFD, s.base, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err == nil {
		err = checkPrivateFile(current)
		_ = syscall.Close(current)
	}
	if err != nil && !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("%w: hosting state target is unsafe", ErrStore)
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return fmt.Errorf("%w: creating hosting temporary file", ErrStore)
	}
	tmp := ".hosting-state-" + hex.EncodeToString(entropy[:]) + ".tmp"
	fd, err := syscall.Openat(dirFD, tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("%w: creating hosting temporary file", ErrStore)
	}
	f := os.NewFile(uintptr(fd), tmp)
	defer f.Close()
	defer syscall.Unlinkat(dirFD, tmp)
	if n, err := s.writeFile(f, b); err != nil || n != len(b) {
		return fmt.Errorf("%w: writing hosting temporary file", ErrStore)
	}
	if err := s.syncFile(f); err != nil {
		return fmt.Errorf("%w: syncing hosting temporary file", ErrStore)
	}
	if err := s.closeFile(f); err != nil {
		return fmt.Errorf("%w: closing hosting temporary file", ErrStore)
	}
	if err := s.rename(dirFD, tmp, dirFD, s.base); err != nil {
		return fmt.Errorf("%w: replacing hosting state", ErrStore)
	}
	if err := s.syncDir(s.dir); err != nil {
		return fmt.Errorf("%w: %w: syncing hosting commit directory", ErrStore, errCommitUncertain)
	}
	return nil
}

func (s *fileStore) close() error {
	// Keep the lock inode on disk. Removing it would allow two processes to
	// acquire locks on different inodes for the same store pathname.
	lockErr := s.lock.Close()
	dirErr := s.dir.Close()
	if lockErr != nil || dirErr != nil {
		return fmt.Errorf("%w: closing hosting state", ErrStore)
	}
	return nil
}
