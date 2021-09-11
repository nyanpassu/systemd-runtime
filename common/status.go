package common

import (
	"context"
	"encoding/json"
	"os"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"

	"github.com/projecteru2/systemd-runtime/utils"
)

const (
	bundleStatusFileName = "status.lock"
)

type BundleStatus struct {
	PID      int
	Created  bool
	Exited   *runtime.Exit
	Disabled bool
}

type BundleStatusManager struct {
	AddressFifo  *os.File
	BundleStatus *os.File
}

func (m BundleStatusManager) ShimStart(ctx context.Context, pid int) (created bool, err error) {
	// First we shall lock the status file
	if err := utils.FileBLock(ctx, m.BundleStatus); err != nil {
		return created, err
	}
	defer func() {
		// panic when we can't unlock status lock
		if err := utils.FileUnlock(m.BundleStatus); err != nil {
			log.G(ctx).Fatalln(errors.Wrap(err, "unlock bundle status error"))
		}
	}()
	status := &BundleStatus{}
	if _, err := utils.FileReadJSON(m.BundleStatus, status); err != nil {
		return created, err
	}
	if status.Disabled {
		return created, ErrBundleDisabled
	}
	if err := utils.FileBLock(ctx, m.AddressFifo); err != nil {
		return created, err
	}
	status.PID = pid
	created = status.Created
	if !status.Created {
		status.Created = true
	}
	return created, utils.FileWriteJSON(m.BundleStatus, status)
}

func (m BundleStatusManager) ShimShutdown() error {
	return utils.FileUnlock(m.BundleStatus)
}

func (m BundleStatusManager) DisableBundle(ctx context.Context) (shimRunning bool, err error) {
	// First we shall lock the status file
	if err := utils.FileBLock(ctx, m.BundleStatus); err != nil {
		return shimRunning, err
	}
	defer func() {
		// panic when we can't unlock status lock
		if err := utils.FileUnlock(m.BundleStatus); err != nil {
			log.G(ctx).WithError(err).Error("unlock bundle status error")
		}
	}()
	status := &BundleStatus{}
	if _, err := utils.FileReadJSON(m.BundleStatus, status); err != nil {
		return shimRunning, err
	}
	if status.Disabled {
		return shimRunning, nil
	}
	status.Disabled = true
	var fifoLockAquired bool
	if shimRunning, err = utils.FileNBLock(m.AddressFifo); err != nil {
		return
	}
	return utils.FileWriteJSON(m.BundleStatus, status)
}

type BundleStatusCAS = func(*BundleStatus) (BundleStatus, bool)

func (l PIDLock) Unlock(logger *logrus.Entry) {
	if err := l.L.Unlock(); err != nil {
		logger.WithField("FileName", l.L.File.Name()).WithError(err).Error("unlock file error")
	}
}

func (l PIDLock) Disable(ctx context.Context) (bool, error) {
	// try nblock first
	if err := l.L.BLock(ctx); err != nil {
		return err
	}
	defer l.Unlock(log.G(ctx))

	status := &PIDLockStatus{}
	if _, err := l.L.ReadJSON(status); err != nil {
		return err
	}
	status.Disabled = true
	return l.L.WriteJSON(status)
}

func (l PIDLock) Disabled(ctx context.Context) (bool, error) {
	// pid lock maybe hold by containerd or running shim
	// in order to wait for containerd finishes its operation, we should aquire the lock
	if err := l.L.BLock(ctx); err != nil {
		return false, err
	}
	defer l.Unlock(log.G(ctx))

	ch := make(chan interface{}, 1)
	go func() {
		defer close(ch)

		if err := l.lock.Lock(); err != nil {
			ch <- err
			return
		}
		// we have locked the pid file

		status := &PIDLockStatus{}
		if ok, err := l.lock.ReadJSON(status); err != nil {
			logrus.WithField("FileName", l.filename).WithError(err).Error("read pid file error")
			ch <- err
		} else if ok {
			ch <- status
		} else {
			ch <- nil
		}
	}()
	select {
	case msg := <-ch:
		if msg == nil {
			return false, nil
		}
		if err, ok := msg.(error); ok {
			return false, err
		}
		return msg.(*PIDLockStatus).Disabled, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func BundleFile(bundlePath string) (*os.File, error) {
	return os.OpenFile(bundlePath+"/"+bundleStatusFileName, os.O_RDWR|os.O_CREATE, 0666)
}

func GetBundleStatus(ctx context.Context, bundleFile *os.File) (s *BundleStatus, err error) {
	var (
		lock utils.Flock
		data []byte
	)
	lock, err = utils.NewFlock()
	if err != nil {
		return
	}
	if err = lock.Lock(); err != nil {
		return
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			log.G(ctx).WithField(
				"bundlePath", bundlePath,
			).WithError(
				err,
			).Error(
				"unlock disabled status file lock error",
			)
		}
	}()
	data, err = lock.Read()
	if err != nil {
		return nil, err
	}
	var status *BundleStatus
	if string(data) != "" {
		status = &BundleStatus{}
		if err = json.Unmarshal(data, &status); err != nil {
			return nil, err
		}
	}
	return status, nil
}

func BundleStarted(ctx context.Context, bundlePath string) (disabled bool, err error) {
	var prev *BundleStatus
	prev, err = UpdateBundleStatus(ctx, bundlePath, func(prev *BundleStatus) (BundleStatus, bool) {
		if prev.Disabled {
			return *prev, false
		}
		prev.Started = true
		return *prev, true
	})
	if err != nil {
		return
	}
	return prev.Disabled, nil
}

// UpdateBundleStatus if mapping return newStatus, true, then newStatus will saved as new bundle status
// Return prev status
func UpdateBundleStatus(ctx context.Context, bundlePath string, cas BundleStatusCAS) (s *BundleStatus, err error) {
	var (
		lock utils.Flock
		data []byte
	)
	lock, err = utils.NewFlock(bundlePath+"/"+bundleStatusFileName, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return
	}
	if err = lock.Lock(); err != nil {
		return
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			log.G(ctx).WithField(
				"bundlePath", bundlePath,
			).WithError(
				err,
			).Error(
				"unlock disabled status file lock error",
			)
		}
	}()
	data, err = lock.Read()
	if err != nil {
		return nil, err
	}

	var status *BundleStatus
	if string(data) != "" {
		status = &BundleStatus{}
		_ = json.Unmarshal(data, &status)
	}

	newStatus, update := cas(status)
	if update {
		data, err = json.Marshal(newStatus)
		if err != nil {
			return nil, err
		}
		log.G(ctx).WithField(
			"bundlePath", bundlePath,
		).WithField(
			"newStatus", string(data),
		).Info(
			"new status for disabled lock",
		)
		if err = lock.Write(data); err != nil {
			return
		}
	}
	return status, nil
}
