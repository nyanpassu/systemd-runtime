package bundle

import (
	"context"
	"os"
	"time"

	"github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/runtime"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/systemd"
	"github.com/projecteru2/systemd-runtime/task"
)

type Bundle struct {
	id                     string
	path                   string
	namespace              string
	containerdAddress      string
	containerdTTRPCAddress string
	statusManager          *common.StatusManager
	exchange               *exchange.Exchange
}

func (b *Bundle) ID() string {
	return b.id
}

func (b *Bundle) Path() string {
	return b.path
}

func (b *Bundle) Namespace() string {
	return b.namespace
}

func (b *Bundle) Cleanup(ctx context.Context) error {
	status, locked, err := b.statusManager.LockForCleanup(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := b.statusManager.ReleaseLocks(); err != nil {
			log.G(ctx).WithError(err).Error("release locks error")
		}
	}()
	if !locked {
		return nil
	}
	if !status.Disabled {
		log.G(ctx).Debug("publish pause event")
		b.exchange.Publish(namespaces.WithNamespace(ctx, b.namespace), runtime.TaskPausedEventTopic, &events.TaskPaused{
			ContainerID: b.ID(),
		})
	}
	_, err = common.Cleanup(ctx, b.id, b.namespace, b.path, false, log.G(ctx))
	return err
}

func (b *Bundle) Delete(context.Context) error {
	return common.DeleteBundlePath(b.path)
}

func (b *Bundle) SaveOpts(ctx context.Context, opts runtime.CreateOpts) error {
	return common.SaveOpts(ctx, b.path, opts)
}

func (b *Bundle) LoadOpts(ctx context.Context) (runtime.CreateOpts, error) {
	return common.LoadOpts(ctx, b.path)
}

func (b *Bundle) CheckContainerdConfig(
	ctx context.Context,
	containerdAddress, containerdTTRPCAddress string,
) error {
	return nil
}

func (b *Bundle) Disable(ctx context.Context) (status common.ShimStatus, shimRunning bool, err error) {
	status, shimRunning, err = b.statusManager.LockForTaskManager(ctx)
	if err != nil {
		return
	}
	defer func() {
		if err := b.statusManager.UnlockStatusFile(); err != nil {
			log.G(ctx).WithError(err).Error("unlock status file error")
		}
	}()
	if status.Disabled {
		return
	}
	newStatus := status
	newStatus.Disabled = true
	if !shimRunning && status.Exit == nil {
		status.Exit = &runtime.Exit{
			Pid:       uint32(status.PID),
			Status:    0,
			Timestamp: time.Now(),
		}
	}
	return status, shimRunning, b.statusManager.UpdateStatus(ctx, newStatus)
}

func (b *Bundle) Exited(ctx context.Context) (lastExit *runtime.Exit, shimRunning bool, err error) {
	status, shimRunning, err := b.statusManager.LockForTaskManager(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if err := b.statusManager.UnlockStatusFile(); err != nil {
			log.G(ctx).WithError(err).Error("unlock status file error")
		}
	}()

	return status.Exit, shimRunning, nil
}

func (b *Bundle) LoadTask(ctx context.Context, events *exchange.Exchange) (runtime.Task, error) {
	status, running, err := b.ShimStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status.Disabled && !running {
		return nil, common.ErrBundleDisabled
	}

	unit, err := systemd.GetUnit(ctx, systemd.UnitName(b.id))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if unit, err = b.recreateSystemdUnit(ctx); err != nil {
			return nil, err
		}
		if err := unit.Enable(ctx); err != nil {
			return nil, err
		}
	}

	return task.NewTask(b, unit, events), nil
}

func (b *Bundle) CreateSystemdUnit(ctx context.Context, opts runtime.CreateOpts) (*systemd.Unit, error) {
	args := []string{"-id", b.id}
	if logrus.GetLevel() == logrus.DebugLevel {
		args = append(args, "-debug")
	}
	args = append(args, "start")

	cmd, err := common.SystemdCommand(b.namespace, opts.Runtime, b.containerdAddress, b.containerdTTRPCAddress, b.path, nil, args...)
	if err != nil {
		return nil, err
	}
	ExecStart := []string{cmd.CmdPath}
	ExecStart = append(ExecStart, cmd.Args...)
	return systemd.Create(
		ctx,
		systemd.UnitName(b.id),
		systemd.Detail{
			Unit: systemd.UnitSector{
				Description: b.id,
			},
			Service: systemd.ServiceSector{
				Type:             "exec",
				WorkingDirectory: cmd.WorkingPath,
				Environment:      cmd.Env,
				ExecStart:        ExecStart,
			},
			Install: systemd.InstallSector{
				WantedBy: "multi-user.target",
			},
		},
	)
}

// Detail .
func (b *Bundle) IntoDetail() systemd.Detail {
	return systemd.Detail{
		Unit: systemd.UnitSector{
			Description: "EruSystemdUnit-" + b.ID(),
		},
		Service: systemd.ServiceSector{
			Type:      "exec",
			ExecStart: []string{systemd.ShimBinaryName},
		},
		Install: systemd.InstallSector{
			WantedBy: "multi-user.target",
		},
	}
}

func (b *Bundle) ShimStatus(ctx context.Context) (status common.ShimStatus, shimRunning bool, err error) {
	status, shimRunning, err = b.statusManager.LockForTaskManager(ctx)
	if err != nil {
		return
	}
	defer func() {
		if err := b.statusManager.UnlockStatusFile(); err != nil {
			log.G(ctx).WithError(err).Error("unlock status file error")
		}
	}()
	if status.Disabled && status.Exit == nil {
		status.Exit = &runtime.Exit{
			Pid:       uint32(status.PID),
			Status:    0,
			Timestamp: time.Now(),
		}
		if err := b.statusManager.UpdateStatus(ctx, status); err != nil {
			log.G(ctx).WithError(err).Error("correct shim status error")
		}
	}
	return status, shimRunning, nil
}

func (b *Bundle) recreateSystemdUnit(ctx context.Context) (*systemd.Unit, error) {
	log.G(ctx).Warn("systemd unit not exists, create new one")
	opts, err := b.LoadOpts(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "load opts error")
	}
	return b.CreateSystemdUnit(ctx, opts)
}
