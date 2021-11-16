package common

import (
	"context"
	"os"
	"sync"

	"github.com/projecteru2/systemd-runtime/utils"
	logrus "github.com/sirupsen/logrus"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"
)

const (
	shimStatusFileName = "shim.status"
	shimLockFileName   = "shim.lock"
)

type ShimStatus struct {
	PID      int
	Created  bool
	Disabled bool
	Exit     *runtime.Exit
}

func openShimStatusFile(bundlePath string) (*os.File, error) {
	return os.OpenFile(bundlePath+"/"+shimStatusFileName, os.O_RDWR|os.O_CREATE, 0666)
}

func openShimLockFile(bundlePath string) (*os.File, error) {
	return os.OpenFile(bundlePath+"/"+shimLockFileName, os.O_RDWR|os.O_CREATE, 0666)
}

type StatusManager struct {
	mu          sync.Mutex
	statusFile  *os.File
	runningFile *os.File

	statusFileLocked  bool
	runningFileLocked bool
}

func NewStatusManager(bundlePath string, logger *logrus.Entry) (*StatusManager, error) {
	statusFile, err := openShimStatusFile(bundlePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			if err := statusFile.Close(); err != nil {
				logger.WithError(err).Errorf("close status file(bundlePath = %s) error", bundlePath)
			}
		}
	}()
	runningFile, err := openShimLockFile(bundlePath)
	if err != nil {
		return nil, err
	}
	return &StatusManager{
		statusFile:  statusFile,
		runningFile: runningFile,
	}, nil
}

func (mng *StatusManager) LockForStartShim(ctx context.Context) (status ShimStatus, err error) {
	if err := mng.lockStatusFile(ctx); err != nil {
		return status, err
	}

	defer func() {
		if err != nil {
			if err := mng.UnlockStatusFile(); err != nil {
				log.G(ctx).WithError(err).Error("unlock status file error")
			}
		}
	}()

	if _, err := utils.FileReadJSON(mng.statusFile, &status); err != nil {
		return status, err
	}
	if status.Disabled {
		return status, ErrBundleDisabled
	}

	succ, err := mng.lockRunningFile()
	if err != nil {
		return status, err
	}
	if !succ {
		return status, ErrBundleIsAlreadyRunning
	}
	return status, nil
}

func (mng *StatusManager) UpdateStatus(ctx context.Context, status ShimStatus) (err error) {
	if err := mng.lockStatusFile(ctx); err != nil {
		return err
	}

	defer func() {
		if err != nil {
			if err := mng.UnlockStatusFile(); err != nil {
				log.G(ctx).WithError(err).Error("unlock status file error")
			}
		}
	}()

	return utils.FileWriteJSON(mng.statusFile, &status)
}

func (mng *StatusManager) LockForCleanup(ctx context.Context) (status ShimStatus, locked bool, err error) {
	if err := mng.lockStatusFile(ctx); err != nil {
		return status, locked, err
	}

	defer func() {
		if err != nil || !locked {
			if err := mng.UnlockStatusFile(); err != nil {
				log.G(ctx).WithError(err).Error("unlock status file error")
			}
		}
	}()

	if _, err := utils.FileReadJSON(mng.statusFile, &status); err != nil {
		return status, locked, err
	}

	succ, err := mng.lockRunningFile()
	if err != nil {
		return status, locked, err
	}
	if !succ {
		return status, false, err
	}
	return status, true, nil
}

func (mng *StatusManager) LockForTaskManager(ctx context.Context) (status ShimStatus, shimRunning bool, err error) {
	if err := mng.lockStatusFile(ctx); err != nil {
		return status, shimRunning, err
	}

	defer func() {
		if err != nil {
			if err := mng.UnlockStatusFile(); err != nil {
				log.G(ctx).WithError(err).Error("unlock status file error")
			}
		}
	}()

	if _, err := utils.FileReadJSON(mng.statusFile, &status); err != nil {
		return status, shimRunning, err
	}

	shimRunning, err = mng.IsShimRunning()
	return
}

func (mng *StatusManager) LockForUpdateStatus(ctx context.Context) (status ShimStatus, err error) {
	if err := mng.lockStatusFile(ctx); err != nil {
		return status, err
	}

	defer func() {
		if err != nil {
			if err := mng.UnlockStatusFile(); err != nil {
				log.G(ctx).WithError(err).Error("unlock status file error")
			}
		}
	}()

	_, err = utils.FileReadJSON(mng.statusFile, &status)
	return
}

func (mng *StatusManager) GetStatus(ctx context.Context) (status ShimStatus, err error) {
	if err := mng.lockStatusFile(ctx); err != nil {
		return status, err
	}

	defer func() {
		if err := mng.UnlockStatusFile(); err != nil {
			log.G(ctx).WithError(err).Error("unlock status file error")
		}
	}()

	_, err = utils.FileReadJSON(mng.statusFile, &status)
	return
}

func (mng *StatusManager) IsShimRunning() (running bool, err error) {
	mng.mu.Lock()
	defer mng.mu.Unlock()

	if mng.runningFileLocked {
		return true, nil
	}
	canLock, err := utils.FileCanLock(mng.runningFile)
	return !canLock, err
}

func (mng *StatusManager) UnlockStatusFile() error {
	mng.mu.Lock()
	defer mng.mu.Unlock()

	if !mng.statusFileLocked {
		return nil
	}

	mng.statusFileLocked = false
	return utils.FileUnlock(mng.statusFile)
}

func (mng *StatusManager) UnlockRunningFile() error {
	mng.mu.Lock()
	defer mng.mu.Unlock()

	if !mng.runningFileLocked {
		return nil
	}

	mng.runningFileLocked = false
	return utils.FileUnlock(mng.runningFile)
}

func (mng *StatusManager) ReleaseLocks() error {
	err := mng.UnlockStatusFile()
	err2 := mng.UnlockRunningFile()
	if err != nil {
		return err
	}
	return err2
}

func (mng *StatusManager) lockStatusFile(ctx context.Context) (err error) {
	mng.mu.Lock()
	defer mng.mu.Unlock()

	if mng.statusFileLocked {
		return nil
	}
	if err := utils.FileLock(ctx, mng.statusFile, 0); err != nil {
		return err
	}
	mng.statusFileLocked = true
	return
}

func (mng *StatusManager) lockRunningFile() (succ bool, err error) {
	mng.mu.Lock()
	defer mng.mu.Unlock()

	if mng.runningFileLocked {
		return true, nil
	}
	succ, err = utils.FileNBLock(mng.runningFile)
	if err != nil {
		return false, err
	}
	mng.runningFileLocked = true
	return succ, err
}
