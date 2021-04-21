package task

import (
	"context"
	"time"

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
	LoadTimeout     = "io.containerd.timeout.shim.load"
	CleanupTimeout  = "io.containerd.timeout.shim.cleanup"
	ShutdownTimeout = "io.containerd.timeout.shim.shutdown"
)

func init() {
	timeout.Set(LoadTimeout, 5*time.Second)
	timeout.Set(CleanupTimeout, 5*time.Second)
	timeout.Set(ShutdownTimeout, 3*time.Second)
}

type TaskService struct {
	taskPid     uint32
	id          string
	namespace   string
	taskService taskv2.TaskService
}

// ID of bundle, same as container
func (t *TaskService) ID() string {
	return t.id
}

// State returns the process state
func (t *TaskService) State(ctx context.Context) (runtime.State, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::State")

	response, err := t.taskService.State(ctx, &taskv2.StateRequest{
		ID: t.ID(),
	})
	if err != nil {
		log.G(ctx).WithField("id", t.ID()).WithError(err).Error("State error")
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
func (t *TaskService) Kill(ctx context.Context, signal uint32, all bool) error {
	log.G(ctx).WithField(
		"id", t.ID(),
	).WithField(
		"signal", signal,
	).WithField(
		"all", all,
	).Debug("call TaskService::Kill")
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
func (t *TaskService) ResizePty(ctx context.Context, size runtime.ConsoleSize) error {
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
func (t *TaskService) CloseIO(ctx context.Context) error {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::CloseIO")
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
func (t *TaskService) Start(ctx context.Context) error {
	log.G(ctx).WithField("id", t.ID()).Error("should not call TaskService::Start, service will start by itself")
	return errdefs.ErrNotImplemented
}

// Wait for the process to exit
func (t *TaskService) Wait(ctx context.Context) (*runtime.Exit, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Wait")

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
func (t *TaskService) Delete(ctx context.Context) (_ *runtime.Exit, err error) {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Delete")
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

	return &runtime.Exit{
		Status:    response.ExitStatus,
		Timestamp: response.ExitedAt,
		Pid:       response.Pid,
	}, nil
}

// PID of the process
func (t *TaskService) PID() uint32 {
	return t.taskPid
}

// Namespace that the task exists in
func (t *TaskService) Namespace() string {
	return t.namespace
}

// Exec adds a process into the container
func (t *TaskService) Exec(ctx context.Context, id string, opts runtime.ExecOpts) (runtime.Process, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Exec")

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
func (t *TaskService) Pids(ctx context.Context) ([]runtime.ProcessInfo, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Pids")
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
func (t *TaskService) Checkpoint(ctx context.Context, path string, options *types.Any) error {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Checkpoint")
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
func (t *TaskService) Update(ctx context.Context, resources *types.Any, _ map[string]string) error {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Update")
	if _, err := t.taskService.Update(ctx, &taskv2.UpdateTaskRequest{
		ID:        t.ID(),
		Resources: resources,
	}); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Process returns a process within the task for the provided id
func (t *TaskService) Process(ctx context.Context, execid string) (runtime.Process, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Process")
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
func (t *TaskService) Stats(ctx context.Context) (*types.Any, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("call TaskService::Stats")
	response, err := t.taskService.Stats(ctx, &taskv2.StatsRequest{
		ID: t.ID(),
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return response.Stats, nil
}
