package common

import (
	"context"
	"encoding/json"
	"os"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"
	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/utils"
)

const (
	fifoFileName         = "fifo_address"
	bundleStatusFileName = "status.lock"
)

var (
	ErrTaskNotExited = errors.New("task not exited")
)

type BundleStatus struct {
	Started  bool
	Exited   *runtime.Exit
	Disabled bool
}

type Bundle interface {
	ID() string
	Namespace() string
	Path() string
	Delete(context.Context) error
	Disable(context.Context) (bool, error)
	Exited(context.Context) (*runtime.Exit, error)
	LoadTask(context.Context, *exchange.Exchange) (runtime.Task, error)
	SaveOpts(context.Context, runtime.CreateOpts) error
	LoadOpts(context.Context) (runtime.CreateOpts, error)
}

func AddressFIFOPath(bundlePath string) string {
	return bundlePath + "/" + fifoFileName
}

func SendAddressOverFifo(ctx context.Context, bundlePath string, addr string) error {
	return utils.SendContentOverFifo(ctx, AddressFIFOPath(bundlePath), addr)
}

func ReceiveAddressOverFifo(ctx context.Context, bundlePath string) (string, error) {
	return utils.ReceiveContentOverFifo(ctx, AddressFIFOPath(bundlePath))
}

func DisableBundle(ctx context.Context, bundlePath string) (started bool, err error) {
	var prev *BundleStatus
	prev, err = updateBundleStatus(ctx, bundlePath, func(prev *BundleStatus) (BundleStatus, bool) {
		if prev.Disabled {
			return BundleStatus{}, false
		}
		b := *prev
		b.Disabled = true
		return b, true
	})
	if err != nil {
		return
	}
	return prev.Started, nil
}

func BundleStarted(ctx context.Context, bundlePath string) (disabled bool, err error) {
	var prev *BundleStatus
	prev, err = updateBundleStatus(ctx, bundlePath, func(prev *BundleStatus) (BundleStatus, bool) {
		if prev.Disabled {
			return BundleStatus{}, false
		}
		b := *prev
		b.Started = true
		return b, true
	})
	if err != nil {
		return
	}
	return prev.Disabled, nil
}

func WriteExited(ctx context.Context, bundlePath string, exit *runtime.Exit) (err error) {
	_, err = updateBundleStatus(ctx, bundlePath, func(prev *BundleStatus) (BundleStatus, bool) {
		b := *prev
		b.Exited = exit
		return b, true
	})
	return err
}

func ReadExited(ctx context.Context, bundlePath string) (exit *runtime.Exit, err error) {
	log.G(ctx).WithField("bundlePath", bundlePath).Debug("read exited from file")
	var prev *BundleStatus
	if prev, err = updateBundleStatus(ctx, bundlePath, func(prev *BundleStatus) (BundleStatus, bool) {
		return BundleStatus{}, false
	}); err != nil {
		return nil, err
	}
	if prev.Exited == nil {
		return nil, ErrTaskNotExited
	}
	return prev.Exited, nil
}

func updateBundleStatus(ctx context.Context, bundlePath string, mapping func(*BundleStatus) (BundleStatus, bool)) (s *BundleStatus, err error) {
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
	status := BundleStatus{}
	if string(data) != "" {
		_ = json.Unmarshal(data, &status)
	}

	newStatus, update := mapping(&status)
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
	return &status, nil
}
