package platformrt

import (
	"context"
	"sync"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/pkg/timeout"
	"github.com/containerd/containerd/runtime"
	taskapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"

	"github.com/gogo/protobuf/types"

	"github.com/projecteru2/systemd-runtime/runshim"
	"github.com/projecteru2/systemd-runtime/systemd"
	"github.com/projecteru2/systemd-runtime/task"
	"github.com/projecteru2/systemd-runtime/utils"
)

type pendingTask struct {
	lock sync.Mutex
	task runtime.Task

	bundle   task.Bundle
	unit     *systemd.Unit
	tasks    task.Tasks
	events   *exchange.Exchange
	launcher task.TaskLauncher
}

// ID of the process
func (t *pendingTask) ID() string {
	return t.bundle.ID()
}

// State returns the process state
func (t *pendingTask) State(context.Context) (runtime.State, error) {
	return runtime.State{
		Status: runtime.CreatedStatus,
	}, nil
}

// Kill signals a container
func (t *pendingTask) Kill(context.Context, uint32, bool) error {
	return errdefs.ErrNotImplemented
}

// Pty resizes the processes pty/console
func (t *pendingTask) ResizePty(context.Context, runtime.ConsoleSize) error {
	return errdefs.ErrNotImplemented
}

// CloseStdin closes the processes stdin
func (t *pendingTask) CloseIO(context.Context) error {
	return errdefs.ErrNotImplemented
}

// Start the container's user defined process
func (t *pendingTask) Start(ctx context.Context) error {
	ta := t.getTask()

	if ta != nil {
		return ta.Start(ctx)
	}

	if err := t.unit.Start(ctx); err != nil {
		return err
	}

	ta, err := t.loadAndReplaceSelf(ctx)
	if err != nil {
		return err
	}
	return ta.Start(ctx)
}

// Wait for the process to exit
func (t *pendingTask) Wait(context.Context) (*runtime.Exit, error) {
	return nil, errdefs.ErrNotImplemented
}

// Delete deletes the process
func (t *pendingTask) Delete(ctx context.Context) (*runtime.Exit, error) {
	if err := t.unit.Remove(ctx); err != nil {
		return nil, err
	}
	return t.launcher.Delete(ctx)
}

// PID of the process
func (t *pendingTask) PID() uint32 {
	return 0
}

// Namespace that the task exists in
func (t *pendingTask) Namespace() string {
	return t.bundle.Namespace()
}

// Pause pauses the container process
func (t *pendingTask) Pause(context.Context) error {
	return errdefs.ErrNotImplemented
}

// Resume unpauses the container process
func (t *pendingTask) Resume(context.Context) error {
	return errdefs.ErrNotImplemented
}

// Exec adds a process into the container
func (t *pendingTask) Exec(context.Context, string, runtime.ExecOpts) (runtime.Process, error) {
	return nil, errdefs.ErrNotImplemented
}

// Pids returns all pids
func (t *pendingTask) Pids(context.Context) ([]runtime.ProcessInfo, error) {
	return nil, errdefs.ErrNotImplemented
}

// Checkpoint checkpoints a container to an image with live system data
func (t *pendingTask) Checkpoint(context.Context, string, *types.Any) error {
	return errdefs.ErrNotImplemented
}

// Update sets the provided resources to a running task
func (t *pendingTask) Update(context.Context, *types.Any) error {
	return errdefs.ErrNotImplemented
}

// Process returns a process within the task for the provided id
func (t *pendingTask) Process(context.Context, string) (runtime.Process, error) {
	return nil, errdefs.ErrNotImplemented
}

// Stats returns runtime specific metrics for a task
func (t *pendingTask) Stats(context.Context) (*types.Any, error) {
	return nil, errdefs.ErrNotImplemented
}

func (t *pendingTask) getTask() runtime.Task {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.task
}

func (t *pendingTask) reset() {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.task = nil
}

func (t *pendingTask) loadAndReplaceSelf(ctx context.Context) (runtime.Task, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.task != nil {
		return t.task, nil
	}

	addr, err := utils.ReceiveAddressOverFifo(ctx, t.bundle.Path())
	if err != nil {
		log.G(ctx).WithError(err).Error("Create container failed")
		return nil, err
	}
	conn, err := runshim.Connect(addr, runshim.AnonReconnectDialer)
	if err != nil {
		log.G(ctx).WithError(err).Error("Connect with shim failed")
		return nil, err
	}
	client := ttrpc.NewClient(conn, ttrpc.WithOnClose(func() {
		ctx := context.Background()

		t.reset()
		t.tasks.Replace(ctx, t.bundle.ID(), t)
		t.loadAndReplaceSelf(ctx)
	}))
	taskClient := taskapi.NewTaskClient(client)

	ctx, cancel := timeout.WithContext(ctx, task.LoadTimeout)
	defer cancel()
	taskPid, err := connect(ctx, taskClient, t.bundle.ID())
	if err != nil {
		log.G(ctx).WithError(err).Error("Connect with shim failed")
		return nil, err
	}
	ta := task.NewTask(taskPid, t.bundle, t.events, taskClient, t.tasks, client)
	t.task = ta
	t.tasks.Replace(ctx, t.bundle.ID(), ta)
	return ta, nil
}
