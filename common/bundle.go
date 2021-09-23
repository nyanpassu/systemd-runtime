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
	Disable(context.Context) (bool, *ShimStatus, error)
	Disabled(context.Context) (disabled bool, running bool, err error)
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

func WriteExited(ctx context.Context, bundlePath string, exit *runtime.Exit) (err error) {
	statusFile, err := OpenShimStatusFile(bundlePath)
	if err != nil {
		return err
	}
	if err := utils.FileLock(ctx, statusFile); err != nil {
		return err
	}
	defer func() {
		if err := utils.FileUnlock(statusFile); err != nil {
			log.G(ctx).WithError(err).Error("unlock status file error")
		}
	}()
	status := ShimStatus{}
	if _, err := utils.FileReadJSON(statusFile, &status); err != nil {
		return err
	}
	status.Exit = exit
	return utils.FileWriteJSON(statusFile, &status)
}

func ReadExited(ctx context.Context, bundlePath string) (exit *runtime.Exit, err error) {
	statusFile, err := OpenShimStatusFile(bundlePath)
	if err != nil {
		return nil, err
	}
	if err := utils.FileLock(ctx, statusFile); err != nil {
		return nil, err
	}
	defer func() {
		if err := utils.FileUnlock(statusFile); err != nil {
			log.G(ctx).WithError(err).Error("unlock status file error")
		}
	}()
	status := ShimStatus{}
	if _, err := utils.FileReadJSON(statusFile, &status); err != nil {
		return nil, err
	}
	return status.Exit, nil
}
