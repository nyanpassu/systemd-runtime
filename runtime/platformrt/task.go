package platformrt

import (
	"context"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/runtime"
	"github.com/gogo/protobuf/types"
)

type loadingFailedTask struct {
	id string
}

// ID of the process
func (t *loadingFailedTask) ID() string {
	return t.id
}

// State returns the process state
func (t *loadingFailedTask) State(context.Context) (runtime.State, error) {
	// There should be a unknow state
	return runtime.State{}, errdefs.ErrNotImplemented
}

// Kill signals a container
func (t *loadingFailedTask) Kill(context.Context, uint32, bool) error {
	return errdefs.ErrNotImplemented
}

// Pty resizes the processes pty/console
func (t *loadingFailedTask) ResizePty(context.Context, runtime.ConsoleSize) error {
	return errdefs.ErrNotImplemented
}

// CloseStdin closes the processes stdin
func (t *loadingFailedTask) CloseIO(context.Context) error {
	return errdefs.ErrNotImplemented
}

// Start the container's user defined process
func (t *loadingFailedTask) Start(context.Context) error {
	return errdefs.ErrNotImplemented
}

// Wait for the process to exit
func (t *loadingFailedTask) Wait(context.Context) (*runtime.Exit, error) {
	return nil, errdefs.ErrNotImplemented
}

// Delete deletes the process
func (t *loadingFailedTask) Delete(context.Context) (*runtime.Exit, error) {
	return nil, errdefs.ErrNotImplemented
}

// PID of the process
func (t *loadingFailedTask) PID() uint32 {
	return 0
}

// Namespace that the task exists in
func (t *loadingFailedTask) Namespace() string {
	return ""
}

// Pause pauses the container process
func (t *loadingFailedTask) Pause(context.Context) error {
	return errdefs.ErrNotImplemented
}

// Resume unpauses the container process
func (t *loadingFailedTask) Resume(context.Context) error {
	return errdefs.ErrNotImplemented
}

// Exec adds a process into the container
func (t *loadingFailedTask) Exec(context.Context, string, runtime.ExecOpts) (runtime.Process, error) {
	return nil, errdefs.ErrNotImplemented
}

// Pids returns all pids
func (t *loadingFailedTask) Pids(context.Context) ([]runtime.ProcessInfo, error) {
	return nil, errdefs.ErrNotImplemented
}

// Checkpoint checkpoints a container to an image with live system data
func (t *loadingFailedTask) Checkpoint(context.Context, string, *types.Any) error {
	return errdefs.ErrNotImplemented
}

// Update sets the provided resources to a running task
func (t *loadingFailedTask) Update(context.Context, *types.Any) error {
	return errdefs.ErrNotImplemented
}

// Process returns a process within the task for the provided id
func (t *loadingFailedTask) Process(context.Context, string) (runtime.Process, error) {
	return nil, errdefs.ErrNotImplemented
}

// Stats returns runtime specific metrics for a task
func (t *loadingFailedTask) Stats(context.Context) (*types.Any, error) {
	return nil, errdefs.ErrNotImplemented
}
