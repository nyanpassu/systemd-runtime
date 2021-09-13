package common

import (
	"context"
	"os"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"

	"github.com/projecteru2/systemd-runtime/utils"
)

type BundleLock struct {
	BundleStatus *os.File
	ExitStatus   *os.File
}

// ShimStarted call when shim start
func (l *BundleLock) LockForShimStart(ctx context.Context) (created bool, err error) {
	// First we shall lock the status file
	if err := utils.FileLock(ctx, l.BundleStatus); err != nil {
		return created, err
	}
	defer func() {
		// panic when we can't unlock status lock
		if err := utils.FileUnlock(l.BundleStatus); err != nil {
			log.G(ctx).Fatalln(errors.Wrap(err, "unlock bundle status error"))
		}
	}()
	status := &BundleStatus{}
	if _, err := utils.FileReadJSON(l.BundleStatus, status); err != nil {
		return created, err
	}
	if status.Disabled {
		return created, ErrBundleDisabled
	}
	if succ, err := utils.FileNBLock(l.ExitStatus); err != nil {
		return created, err
	} else if !succ {
		// we can't aquire fifo file lock, maybe shim is already started
		return created, ErrAquireFifoFileLockFailed
	}
	status.PID = os.Getpid()
	created = status.Created
	if !status.Created {
		status.Created = true
	}
	return created, utils.FileWriteJSON(l.BundleStatus, status)
}

func (l *BundleLock) ShimShutdown(logger *logrus.Entry, exit *runtime.Exit) error {
	if exit != nil {
		if err := utils.FileWriteJSON(l.ExitStatus, ExitStatus{
			Exit: *exit,
		}); err != nil {
			logger.WithError(err).Error("write exit status error")
		}
	}
	return utils.FileUnlock(l.BundleStatus)
}

func (l *BundleLock) DisableBundle(ctx context.Context) (shimRunning bool, err error) {
	// First we shall lock the status file
	if err := utils.FileLock(ctx, l.BundleStatus); err != nil {
		return shimRunning, err
	}
	defer func() {
		// panic when we can't unlock status lock
		if err := utils.FileUnlock(l.BundleStatus); err != nil {
			log.G(ctx).WithError(err).Error("unlock bundle status error")
		}
	}()
	status := BundleStatus{}
	if _, err := utils.FileReadJSON(l.BundleStatus, &status); err != nil {
		return shimRunning, err
	}
	fileCanLock, err := utils.FileCanLock(l.ExitStatus)
	if err != nil {
		return shimRunning, err
	}
	if !status.Disabled {
		status.Disabled = true
		if err := utils.FileWriteJSON(l.BundleStatus, &status); err != nil {
			return shimRunning, err
		}
	}
	return !fileCanLock, nil
}
