package common

import (
	"context"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"
	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/utils"
)

const (
	fifoFileName = "fifo_address"
)

var (
	ErrTaskNotExited = errors.New("task not exited")
)

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
	prev, err = UpdateBundleStatus(ctx, bundlePath, func(prev *BundleStatus) (BundleStatus, bool) {
		if prev.Disabled {
			return *prev, false
		}
		prev.Disabled = true
		return *prev, true
	})
	if err != nil {
		return
	}
	return prev.Started, nil
}

func WriteExited(ctx context.Context, bundlePath string, exit *runtime.Exit) (err error) {
	_, err = UpdateBundleStatus(ctx, bundlePath, func(prev *BundleStatus) (BundleStatus, bool) {
		prev.Exited = exit
		return *prev, true
	})
	return err
}

func ReadExited(ctx context.Context, bundlePath string) (exit *runtime.Exit, err error) {
	log.G(ctx).WithField("bundlePath", bundlePath).Debug("read exited from file")
	var prev *BundleStatus
	status, err := GetBundleStatus(ctx, bundlePath)
	if err != nil {
		return nil, err
	}
	if status == nil || status.Exited == nil {
		return nil, ErrTaskNotExited
	}
	return prev.Exited, nil
}
