package task

import (
	"context"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/runtime"

	"github.com/gogo/protobuf/types"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/systemd"
)

var (
	ErrTaskIsPaused = errors.New("task is paused")
	ErrTaskIsKilled = errors.New("task is killed")
)

type Task struct {
	bundle common.Bundle
	unit   *systemd.Unit
	logger *logrus.Entry

	connMng  ConnMng
	exchange *exchange.Exchange
}

func NewTask(b common.Bundle, unit *systemd.Unit, exchange *exchange.Exchange) runtime.Task {
	logger := logrus.NewEntry(logrus.StandardLogger()).WithField("id", b.ID()).WithField("namespace", b.Namespace())

	t := &Task{
		bundle: b,
		unit:   unit,
		logger: logger,
		connMng: ConnMng{
			bundle: b,
			logger: logger,
		},
		exchange: exchange,
	}

	go t.connMng.connect()

	return t
}

// ID of the process
func (t *Task) ID() string {
	return t.bundle.ID()
}

// State returns the process state
func (t *Task) State(ctx context.Context) (state runtime.State, err error) {
	t.logger.Debug("get task state")
	defer func() {
		if err == nil {
			t.logger.Debug("task state = %s", StatusIntoString(state.Status))
		}
	}()

	service := t.connMng.getService()
	if service == nil {
		// check whether shim is disabled
		status, running, err := t.bundle.ShimStatus(ctx)
		if err != nil {
			return state, err
		}
		// only return stopped status on disabled and shim process not running
		if status.Disabled && !running {
			return runtime.State{
				Status:     runtime.StoppedStatus,
				ExitStatus: status.Exit.Status,
				ExitedAt:   status.Exit.Timestamp,
			}, nil
		}
		// created field in common.ShimStatus means the container has been created for at least once
		if !status.Created {
			return runtime.State{
				Status: runtime.CreatedStatus,
			}, nil
		}
		// other wise we return pasued
		return runtime.State{
			Status: runtime.PausedStatus,
		}, nil
	}

	return service.State(ctx)
}

// Kill will shutdown a container forever
func (t *Task) Kill(ctx context.Context, sig uint32, all bool) (err error) {
	logger := t.logger.WithField("signal", sig).WithField("all", all)
	logger.Debugf("kill task, signal = %v, all = %v", sig, all)
	defer func() { logger.Debugf("kill task completed, has error = %v", err != nil) }()

	if err = t.unit.DisableIfPresent(ctx); err != nil {
		return err
	}

	status, running, err := t.bundle.Disable(ctx)
	if err != nil {
		logger.WithError(err).Error("[LateInit Kill] disable bundle error")
		return err
	}

	if !running {
		t.connMng.Disabled(status.Exit)
		t.exchange.Publish(ctx, runtime.TaskExitEventTopic, &events.TaskExit{
			ID:          t.ID(),
			ContainerID: t.ID(),
			Pid:         0,
			ExitStatus:  status.Exit.Status,
			ExitedAt:    status.Exit.Timestamp,
		})
		return nil
	}
	t.connMng.Disable()

	logger.Debug("wait service")
	sub, err := t.connMng.subscribeService(ctx)
	if err != nil {
		return err
	}
	if sub.exit != nil {
		return nil
	}

	return sub.service.Kill(ctx, sig, all)
}

// Pty resizes the processes pty/console
func (t *Task) ResizePty(ctx context.Context, size runtime.ConsoleSize) error {
	t.logger.Debug("resize voliatile task pty")

	ta, err := t.connMng.getRunningService(ctx)
	if err != nil {
		return err
	}

	return ta.ResizePty(ctx, size)
}

// CloseStdin closes the processes stdin
func (t *Task) CloseIO(ctx context.Context) error {
	t.logger.Debug("close task io")

	ta, err := t.connMng.getRunningService(ctx)
	if err != nil {
		return err
	}

	return ta.CloseIO(ctx)
}

// Start the container's user defined process
// Consider that our shim is started by it self, so we only need to start the systemd unit, and wait for connection
func (t *Task) Start(ctx context.Context) error {
	t.logger.Debug("start task")

	if err := t.unit.Enable(ctx); err != nil {
		t.logger.WithError(err).Error("enable unit error")
		return err
	}

	if err := t.unit.Start(ctx); err != nil {
		t.logger.WithError(err).Error("Start unit error")
		return err
	}

	_, err := t.connMng.waitServiceConnected(ctx)
	return err
}

// Wait for the process to exit
func (t *Task) Wait(ctx context.Context) (*runtime.Exit, error) {
	t.logger.Debug("wait task exit")

	sub, err := t.connMng.subscribeService(ctx)
	if err != nil {
		return nil, err
	}
	if sub.exit != nil {
		return sub.exit, nil
	}
	return sub.service.Wait(ctx)
}

// Delete deletes the process
func (t *Task) Delete(ctx context.Context) (exit *runtime.Exit, err error) {
	logger := t.logger
	logger.Debug("delete volatile task")

	sub, err := t.connMng.subscribeService(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err == nil {
			if err := t.unit.DeleteIfPresent(ctx); err != nil {
				logger.WithField(
					"systemd-unit-name", t.unit.Name,
				).WithError(
					err,
				).Error(
					"delete systemd unit error",
				)
			}
		}
	}()

	if sub.exit != nil {
		if err := t.bundle.Cleanup(ctx); err != nil {
			t.logger.WithError(err).Error("cleanup bundle error")
		}
		if err := t.bundle.Delete(ctx); err != nil {
			return nil, err
		}
		t.exchange.Publish(ctx, runtime.TaskDeleteEventTopic, &events.TaskDelete{
			ID:          t.ID(),
			ContainerID: t.ID(),
			Pid:         0,
			ExitStatus:  sub.exit.Status,
			ExitedAt:    sub.exit.Timestamp,
		})
		return sub.exit, nil
	}
	return sub.service.Delete(ctx)
}

// PID of the process
func (t *Task) PID() uint32 {
	// always return 0
	return 0
}

// Namespace that the task exists in
func (t *Task) Namespace() string {
	return t.bundle.Namespace()
}

// Pause pauses the container process
func (t *Task) Pause(ctx context.Context) error {
	t.logger.Debug("pausing task")
	return t.unit.Stop(ctx)
}

// Resume unpauses the container process
func (t *Task) Resume(ctx context.Context) error {
	t.logger.Debug("resume task")
	return t.unit.Start(ctx)
}

// Exec adds a process into the container
func (t *Task) Exec(ctx context.Context, id string, opts runtime.ExecOpts) (runtime.Process, error) {
	t.logger.Debug("adds a process into the task")
	s, err := t.connMng.getRunningService(ctx)
	if err != nil {
		return nil, err
	}
	return s.Exec(ctx, id, opts)
}

// Pids returns all pids
func (t *Task) Pids(ctx context.Context) ([]runtime.ProcessInfo, error) {
	t.logger.Debug("list all pids in container")
	s := t.connMng.getService()
	if s == nil {
		return []runtime.ProcessInfo{}, nil
	}
	return s.Pids(ctx)
}

// Checkpoint checkpoints a container to an image with live system data
func (t *Task) Checkpoint(ctx context.Context, path string, opts *types.Any) error {
	t.logger.Debug("checkpoints task")
	s, err := t.connMng.getRunningService(ctx)
	if err != nil {
		return err
	}
	return s.Checkpoint(ctx, path, opts)
}

// Update sets the provided resources to a running task
func (t *Task) Update(ctx context.Context, resources *types.Any, annotations map[string]string) error {
	t.logger.Debug("update task")
	s, err := t.connMng.getRunningService(ctx)
	if err != nil {
		return err
	}
	return s.Update(ctx, resources, annotations)
}

// Process returns a process within the task for the provided id
func (t *Task) Process(ctx context.Context, id string) (runtime.Process, error) {
	t.logger.Debug("get process from task")
	s, err := t.connMng.getRunningService(ctx)
	if err != nil {
		return nil, err
	}
	return s.Process(ctx, id)
}

// Stats returns runtime specific metrics for a task
func (t *Task) Stats(ctx context.Context) (*types.Any, error) {
	t.logger.Debug("get stats from task")
	s, err := t.connMng.getRunningService(ctx)
	if err != nil {
		return nil, err
	}
	return s.Stats(ctx)
}
