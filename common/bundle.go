package common

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/mount"
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
	Cleanup(context.Context) error
	Delete(context.Context) error
	Disable(context.Context) (status ShimStatus, running bool, err error)
	ShimStatus(context.Context) (status ShimStatus, running bool, err error)
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

func DeleteBundlePath(bundlePath string) error {
	work, werr := os.Readlink(filepath.Join(bundlePath, "work"))
	rootfs := filepath.Join(bundlePath, "rootfs")
	if err := mount.UnmountAll(rootfs, 0); err != nil {
		return errors.Wrapf(err, "unmount rootfs %s", rootfs)
	}
	if err := os.Remove(rootfs); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "failed to remove bundle rootfs")
	}
	err := atomicDelete(bundlePath)
	if err == nil {
		if werr == nil {
			return atomicDelete(work)
		}
		return nil
	}
	// error removing the bundle path; still attempt removing work dir
	var err2 error
	if werr == nil {
		err2 = atomicDelete(work)
		if err2 == nil {
			return err
		}
	}
	return errors.Wrapf(err, "failed to remove both bundle and workdir locations: %v", err2)
}

// atomicDelete renames the path to a hidden file before removal
func atomicDelete(path string) error {
	// create a hidden dir for an atomic removal
	atomicPath := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s", filepath.Base(path)))
	if err := os.Rename(path, atomicPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.RemoveAll(atomicPath)
}
