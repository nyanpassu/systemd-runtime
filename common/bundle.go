package common

import (
	"context"

	"github.com/containerd/containerd/events/exchange"
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
	Disable(context.Context) (status ShimStatus, running bool, err error)
	Disabled(context.Context) (disabled bool, running bool, err error)
	Exited(context.Context) (lastExit *runtime.Exit, running bool, err error)
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
