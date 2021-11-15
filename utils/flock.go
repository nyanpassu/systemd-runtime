package utils

import (
	"context"
	"io"
	"os"
	"syscall"
	"time"
)

const (
	defaultLockInterval = time.Duration(100) * time.Millisecond
)

func FileCanLock(file *os.File) (bool, error) {
	flockT := syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_WRLCK,
		Whence: io.SeekStart,
	}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_GETLK, &flockT); err != nil {
		return false, err
	}
	return flockT.Type == syscall.F_UNLCK, nil
}

// FileLock only accept zero or one FileLockOpts
func FileLock(ctx context.Context, file *os.File, interval time.Duration) error {
	if interval == 0 {
		interval = defaultLockInterval
	}
	for {
		if succ, err := FileNBLock(file); err != nil {
			return err
		} else if succ {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func FileBLock(file *os.File) error {
	return syscall.FcntlFlock(file.Fd(), syscall.F_SETLKW, &syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_WRLCK,
		Whence: io.SeekStart,
	})
}

func FileNBLock(file *os.File) (bool, error) {
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_WRLCK,
		Whence: io.SeekStart,
	}); err != nil {
		if err == syscall.EAGAIN || err == syscall.EACCES {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func FileUnlock(file *os.File) error {
	return syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_UNLCK,
		Whence: io.SeekStart,
	})
}
