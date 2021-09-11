package utils

import (
	"context"
	"os"
	"syscall"

	"github.com/sirupsen/logrus"
)

func FileNBLock(file *os.File) (bool, error) {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func FileBLock(ctx context.Context, file *os.File) error {
	lockCh := make(chan error, 1)
	// use another channel to conclude which channel we will select on
	branchCh := make(chan int, 1)

	go func() {
		if err := FileLock(file); err != nil {
			lockCh <- err
			close(lockCh)
			return
		}
		close(lockCh)
		// conclude which channel we have selected
		branch := <-branchCh
		if branch == 1 {
			if err := FileUnlock(file); err != nil {
				logrus.WithField("FileName", file.Name()).WithError(err).Error("unlock filelock error")
			}
		}
	}()
	select {
	case <-ctx.Done():
		branchCh <- 1
		close(branchCh)
		return ctx.Err()
	case err := <-lockCh:
		branchCh <- 2
		close(branchCh)
		return err
	}
}

// FileLock blocking
func FileLock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

// FileUnlock blocking
func FileUnlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
