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

type FileLockOpts struct {
	Interval time.Duration
}

func FileCanLock(file *os.File) (bool, error) {
	flock_t := syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_WRLCK,
		Whence: io.SeekStart,
	}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_GETLK, &flock_t); err != nil {
		return false, err
	}
	return flock_t.Type == syscall.F_UNLCK, nil
}

// FileLock only accept zero or one FileLockOpts
func FileLock(ctx context.Context, file *os.File, opts ...FileLockOpts) error {
	var opt FileLockOpts
	if len(opts) == 0 {
		opt.Interval = defaultLockInterval
	} else {
		opt = opts[0]
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
		case <-time.After(opt.Interval):
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

// func FileNBLock(file *os.File) (bool, error) {
// 	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
// 		if err == syscall.EWOULDBLOCK {
// 			return false, nil
// 		}
// 		return false, err
// 	}
// 	return true, nil
// }

// func FileBLock(ctx context.Context, file *os.File) error {
// 	lockCh := make(chan error, 1)
// 	// use another channel to conclude which channel we will select on
// 	branchCh := make(chan int, 1)

// 	go func() {
// 		if err := FileLock(file); err != nil {
// 			lockCh <- err
// 			close(lockCh)
// 			return
// 		}
// 		close(lockCh)
// 		// conclude which channel we have selected
// 		branch := <-branchCh
// 		if branch == 1 {
// 			if err := FileUnlock(file); err != nil {
// 				logrus.WithField("FileName", file.Name()).WithError(err).Error("unlock filelock error")
// 			}
// 		}
// 	}()
// 	select {
// 	case <-ctx.Done():
// 		branchCh <- 1
// 		close(branchCh)
// 		return ctx.Err()
// 	case err := <-lockCh:
// 		branchCh <- 2
// 		close(branchCh)
// 		return err
// 	}
// }

// // FileLock blocking
// func FileLock(file *os.File) error {
// 	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
// }

// // FileUnlock blocking
// func FileUnlock(file *os.File) error {
// 	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
// }
