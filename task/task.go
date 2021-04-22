package task

import (
	"context"
	"io"
	"time"

	"github.com/containerd/containerd/events/exchange"

	"github.com/nyanpassu/containerd-eru-runtime-plugin/bundle"

	"github.com/containerd/ttrpc"
	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"

	tasktypes "github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/pkg/timeout"
	"github.com/containerd/containerd/runtime"
	taskv2 "github.com/containerd/containerd/runtime/v2/task"
)

const (
	loadTimeout     = "io.containerd.timeout.shim.load"
	cleanupTimeout  = "io.containerd.timeout.shim.cleanup"
	shutdownTimeout = "io.containerd.timeout.shim.shutdown"
)

func init() {
	timeout.Set(loadTimeout, 5*time.Second)
	timeout.Set(cleanupTimeout, 5*time.Second)
	timeout.Set(shutdownTimeout, 3*time.Second)
}

func NewTask(
	ctx context.Context,
	taskPid uint32,
	bundle bundle.Bundle,
	events *exchange.Exchange,
	t taskv2.TaskService,
	tasks Tasks,
	closeable io.Closer,
) runtime.Task {
	return &task{
		taskPid:     taskPid,
		bundle:      bundle,
		taskService: t,
		tasks:       tasks,
		events:      events,
		closeable:   closeable,
	}
}

type task struct {
	taskPid uint32
	bundle  bundle.Bundle

	taskService taskv2.TaskService
	tasks       Tasks
	events      *exchange.Exchange
	closeable   io.Closer
}

// ID of bundle, same as container
func (t *task) ID() string {
	return t.bundle.ID()
}

// State returns the process state
func (t *task) State(ctx context.Context) (runtime.State, error) {
	response, err := t.taskService.State(ctx, &taskv2.StateRequest{
		ID: t.ID(),
	})
	if err != nil {
		if !errors.Is(err, ttrpc.ErrClosed) {
			return runtime.State{}, errdefs.FromGRPC(err)
		}
		return runtime.State{}, errdefs.ErrNotFound
	}
	var status runtime.Status
	switch response.Status {
	case tasktypes.StatusCreated:
		status = runtime.CreatedStatus
	case tasktypes.StatusRunning:
		status = runtime.RunningStatus
	case tasktypes.StatusStopped:
		status = runtime.StoppedStatus
	case tasktypes.StatusPaused:
		status = runtime.PausedStatus
	case tasktypes.StatusPausing:
		status = runtime.PausingStatus
	}
	return runtime.State{
		Pid:        response.Pid,
		Status:     status,
		Stdin:      response.Stdin,
		Stdout:     response.Stdout,
		Stderr:     response.Stderr,
		Terminal:   response.Terminal,
		ExitStatus: response.ExitStatus,
		ExitedAt:   response.ExitedAt,
	}, nil
}

// Kill signals a container
func (t *task) Kill(ctx context.Context, signal uint32, all bool) error {
	if _, err := t.taskService.Kill(ctx, &taskv2.KillRequest{
		ID:     t.ID(),
		Signal: signal,
		All:    all,
	}); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Pty resizes the processes pty/console
func (t *task) ResizePty(ctx context.Context, size runtime.ConsoleSize) error {
	_, err := t.taskService.ResizePty(ctx, &taskv2.ResizePtyRequest{
		ID:     t.ID(),
		Width:  size.Width,
		Height: size.Height,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// CloseStdin closes the processes stdin
func (t *task) CloseIO(ctx context.Context) error {
	_, err := t.taskService.CloseIO(ctx, &taskv2.CloseIORequest{
		ID:    t.ID(),
		Stdin: true,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Start the container's user defined process
func (t *task) Start(ctx context.Context) error {
	response, err := t.taskService.Start(ctx, &taskv2.StartRequest{
		ID: t.ID(),
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	t.taskPid = response.Pid
	return nil
}

// Wait for the process to exit
func (t *task) Wait(ctx context.Context) (*runtime.Exit, error) {
	response, err := t.taskService.Wait(ctx, &taskv2.WaitRequest{
		ID: t.ID(),
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return &runtime.Exit{
		Pid:       uint32(t.taskPid),
		Timestamp: response.ExitedAt,
		Status:    response.ExitStatus,
	}, nil
}

// Delete deletes the process
func (t *task) Delete(ctx context.Context) (*runtime.Exit, error) {
	response, shimErr := t.taskService.Delete(ctx, &taskv2.DeleteRequest{
		ID: t.ID(),
	})
	if shimErr != nil {
		log.G(ctx).WithField("id", t.ID()).WithError(shimErr).Debug("failed to delete task")
		if !errors.Is(shimErr, ttrpc.ErrClosed) {
			shimErr = errdefs.FromGRPC(shimErr)
			if !errdefs.IsNotFound(shimErr) {
				return nil, shimErr
			}
		}
		return nil, shimErr
	}

	if err := t.close(ctx); err != nil {
		return nil, err
	}

	return &runtime.Exit{
		Status:    response.ExitStatus,
		Timestamp: response.ExitedAt,
		Pid:       response.Pid,
	}, nil
}

// PID of the process
func (t *task) PID() uint32 {
	return t.taskPid
}

// Namespace that the task exists in
func (t *task) Namespace() string {
	return t.bundle.Namespace()
}

// Pause pauses the container process
func (t *task) Pause(ctx context.Context) error {
	if _, err := t.taskService.Pause(ctx, &taskv2.PauseRequest{
		ID: t.ID(),
	}); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Resume unpauses the container process
func (t *task) Resume(ctx context.Context) error {
	if _, err := t.taskService.Resume(ctx, &taskv2.ResumeRequest{
		ID: t.ID(),
	}); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Exec adds a process into the container
func (t *task) Exec(ctx context.Context, id string, opts runtime.ExecOpts) (runtime.Process, error) {
	if err := identifiers.Validate(id); err != nil {
		return nil, errors.Wrapf(err, "invalid exec id %s", id)
	}
	request := &taskv2.ExecProcessRequest{
		ID:       t.ID(),
		ExecID:   id,
		Stdin:    opts.IO.Stdin,
		Stdout:   opts.IO.Stdout,
		Stderr:   opts.IO.Stderr,
		Terminal: opts.IO.Terminal,
		Spec:     opts.Spec,
	}
	if _, err := t.taskService.Exec(ctx, request); err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return &process{
		id:   id,
		task: t,
	}, nil
}

// Pids returns all pids
func (t *task) Pids(ctx context.Context) ([]runtime.ProcessInfo, error) {
	resp, err := t.taskService.Pids(ctx, &taskv2.PidsRequest{
		ID: t.ID(),
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	var processList []runtime.ProcessInfo
	for _, p := range resp.Processes {
		processList = append(processList, runtime.ProcessInfo{
			Pid:  p.Pid,
			Info: p.Info,
		})
	}
	return processList, nil
}

// Checkpoint checkpoints a container to an image with live system data
func (t *task) Checkpoint(ctx context.Context, path string, options *types.Any) error {
	request := &taskv2.CheckpointTaskRequest{
		ID:      t.ID(),
		Path:    path,
		Options: options,
	}
	if _, err := t.taskService.Checkpoint(ctx, request); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Update sets the provided resources to a running task
func (t *task) Update(ctx context.Context, resources *types.Any) error {
	if _, err := t.taskService.Update(ctx, &taskv2.UpdateTaskRequest{
		ID:        t.ID(),
		Resources: resources,
	}); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Process returns a process within the task for the provided id
func (t *task) Process(ctx context.Context, execid string) (runtime.Process, error) {
	p := &process{
		id:   execid,
		task: t,
	}
	if _, err := p.State(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// Stats returns runtime specific metrics for a task
func (t *task) Stats(ctx context.Context) (*types.Any, error) {
	response, err := t.taskService.Stats(ctx, &taskv2.StatsRequest{
		ID: t.ID(),
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return response.Stats, nil
}

func (t *task) close(ctx context.Context) error {
	return t.tasks.Delete(ctx, t, func() error {
		if err := t.waitShutdown(ctx); err != nil {
			log.G(ctx).WithField("id", t.ID()).WithError(err).Error("failed to shutdown shim")
		}

		if err := t.bundle.Delete(); err != nil {
			log.G(ctx).WithField("id", t.ID()).WithError(err).Error("failed to delete bundle")
		}

		if err := t.closeable.Close(); err != nil {
			return err
		}

		return nil
	})
}

func (t *task) waitShutdown(ctx context.Context) error {
	ctx, cancel := timeout.WithContext(ctx, shutdownTimeout)
	defer cancel()
	return t.shutdown(ctx)
}

func (t *task) shutdown(ctx context.Context) error {
	_, err := t.taskService.Shutdown(ctx, &taskv2.ShutdownRequest{
		ID: t.ID(),
	})
	if err != nil && !errors.Is(err, ttrpc.ErrClosed) {
		return errdefs.FromGRPC(err)
	}
	return nil
}
