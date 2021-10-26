package common

import (
	"context"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/pkg/process"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	goRunc "github.com/containerd/go-runc"

	"github.com/projecteru2/systemd-runtime/runc"
)

func Cleanup(
	ctx context.Context,
	id string,
	namespace string,
	bundlePath string,
	cleanBundle bool,
	logger *logrus.Entry,
) (*shimapi.DeleteResponse, error) {
	logger.WithField("id", id).Info("Begin Cleanup")
	runtime := "/usr/bin/runc"

	logrus.WithField("id", id).Info("ReadOptions")
	opts, err := runc.ReadOptions(bundlePath)
	if err != nil {
		return nil, err
	}
	root := process.RuncRoot
	if opts != nil && opts.Root != "" {
		root = opts.Root
	}

	logrus.WithField("runtime", runtime).WithField("ns", namespace).WithField("root", root).WithField("path", bundlePath).Info("NewRunc")
	r := newRunc(root, bundlePath, namespace, runtime, "", false)

	logrus.WithField("id", id).Info("Runc Delete")
	if err := r.Delete(ctx, id, &goRunc.DeleteOpts{
		Force: true,
	}); err != nil {
		logrus.WithError(err).Warn("failed to remove runc container")
	}
	logrus.Info("UnmountAll")
	if err := mount.UnmountAll(filepath.Join(bundlePath, "rootfs"), 0); err != nil {
		logrus.WithError(err).Warn("failed to cleanup rootfs mount")
	}
	logrus.Info("Cleanup complete")

	if cleanBundle {
		if err := DeleteBundlePath(bundlePath); err != nil {
			logger.WithError(err).Error("cleanup bundle path error")
		}
	}

	return &shimapi.DeleteResponse{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

func newRunc(root, path, namespace, runtime, criu string, systemd bool) *goRunc.Runc {
	return &goRunc.Runc{
		Command:       runtime,
		Log:           filepath.Join(path, "log.json"),
		LogFormat:     goRunc.JSON,
		PdeathSignal:  unix.SIGKILL,
		Root:          filepath.Join(root, namespace),
		Criu:          criu,
		SystemdCgroup: systemd,
	}
}
