package proxy

import (
	"context"

	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"

	rtapi "github.com/containerd/containerd/api/services/runtime/v1"
	typesapi "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"
)

func NewPlatformRuntime(runtime rtapi.PlatformRuntimeClient, name string) runtime.PlatformRuntime {
	return &proxyPlatformRuntime{
		id: name,
		rt: runtime,
	}
}

type proxyPlatformRuntime struct {
	id        string
	rt        rtapi.PlatformRuntimeClient
	namespace string
}

// ID of the runtime
func (p *proxyPlatformRuntime) ID() string {
	return p.id
}

// Create creates a task with the provided id and options.
func (p *proxyPlatformRuntime) Create(ctx context.Context, id string, opts runtime.CreateOpts) (runtime.Task, error) {
	ns, err := namespaces.NamespaceRequired(ctx)

	var rootfs []*typesapi.Mount
	if opts.Rootfs != nil {
		for _, mnt := range opts.Rootfs {
			rootfs = append(rootfs, &typesapi.Mount{
				Type:    mnt.Type,
				Source:  mnt.Source,
				Options: mnt.Options,
			})
		}
	}
	resp, err := p.rt.Create(ctx, &rtapi.CreateTaskRequest{
		ID:         id,
		Rootfs:     rootfs,
		Stdin:      opts.IO.Stdin,
		Stdout:     opts.IO.Stdout,
		Stderr:     opts.IO.Stderr,
		Terminal:   opts.IO.Terminal,
		Checkpoint: opts.Checkpoint,
		Namespace:  ns,
	})
	if err != nil {
		return nil, err
	}
	return newProxyTask(p.rt, id, resp.Pid, p.namespace), nil
}

// Get returns a task.
func (p *proxyPlatformRuntime) Get(ctx context.Context, id string) (runtime.Task, error) {
	resp, err := p.rt.Get(ctx, &rtapi.GetTaskRequest{
		ID: id,
	})
	if err != nil {
		return nil, err
	}
	// server will not use Process.ContainerID but process.ID
	return newProxyTask(p.rt, resp.Task.Process.ID, resp.Task.Process.Pid, resp.Task.Namespace), nil
}

// Tasks returns all the current tasks for the runtime.
// Any container runs at most one task at a time.
func (p *proxyPlatformRuntime) Tasks(ctx context.Context, b bool) ([]runtime.Task, error) {

	resp, err := p.rt.List(ctx, &rtapi.ListTasksRequest{})
	if err != nil {
		return nil, err
	}
	var tasks []runtime.Task
	for _, task := range resp.Tasks {
		tasks = append(tasks, newProxyTask(p.rt, task.Process.ID, task.Process.Pid, task.Namespace))
	}
	return tasks, nil
}

// Add adds a task into runtime.
func (p *proxyPlatformRuntime) Add(ctx context.Context, task runtime.Task) error {
	return errors.New("unsupported")
}

// Delete remove a task.
func (p *proxyPlatformRuntime) Delete(ctx context.Context, id string) {
	// do nothing
}

func newProxyTask(rt rtapi.PlatformRuntimeClient, id string, pid uint32, namespace string) runtime.Task {
	return &proxyTask{
		proxyProcess: proxyProcess{
			rt:  rt,
			tid: id,
		},
		pid:       pid,
		namespace: namespace,
	}
}

type proxyTask struct {
	proxyProcess
	pid       uint32
	namespace string
}

// ID of the process
func (t *proxyTask) ID() string {
	return t.tid
}

// PID of the process
func (t *proxyTask) PID() uint32 {
	return t.pid
}

// Namespace that the task exists in
func (t *proxyTask) Namespace() string {
	return t.namespace
}

// Start the container's user defined process
func (p *proxyTask) Start(ctx context.Context) error {
	resp, err := p.rt.Start(ctx, &rtapi.StartRequest{
		ID: p.tid,
	})
	if err == nil {
		p.pid = resp.Pid
	}
	return err
}

// Pause pauses the container process
func (t *proxyTask) Pause(ctx context.Context) error {
	_, err := t.rt.Pause(ctx, &rtapi.PauseTaskRequest{
		ID: t.tid,
	})
	return err
}

// Resume unpauses the container process
func (t *proxyTask) Resume(ctx context.Context) error {
	_, err := t.rt.Resume(ctx, &rtapi.ResumeTaskRequest{
		ID: t.tid,
	})
	return err
}

// Exec adds a process into the container
func (t *proxyTask) Exec(ctx context.Context, execID string, opts runtime.ExecOpts) (runtime.Process, error) {
	_, err := t.rt.Exec(ctx, &rtapi.ExecProcessRequest{
		ID:       t.tid,
		ExecID:   execID,
		Stdin:    opts.IO.Stdin,
		Stdout:   opts.IO.Stdout,
		Stderr:   opts.IO.Stderr,
		Terminal: opts.IO.Terminal,
		Spec:     opts.Spec,
	})
	if err != nil {
		return nil, err
	}
	return newProxyProcess(
		t.rt,
		t.tid,
		execID,
	), nil
}

// Pids returns all pids
func (t *proxyTask) Pids(ctx context.Context) ([]runtime.ProcessInfo, error) {
	resp, err := t.rt.Pids(ctx, &rtapi.PidsRequest{
		ID: t.tid,
	})
	if err != nil {
		return nil, err
	}
	var r []runtime.ProcessInfo
	for _, p := range resp.Processes {
		r = append(r, runtime.ProcessInfo{
			Pid:  p.Pid,
			Info: p.Info,
		})
	}
	return r, nil
}

// Checkpoint checkpoints a container to an image with live system data
func (t *proxyTask) Checkpoint(ctx context.Context, path string, any *types.Any) error {
	_, err := t.rt.Checkpoint(ctx, &rtapi.CheckpointTaskRequest{
		ID:      t.tid,
		Path:    path,
		Options: any,
	})
	return err
}

// Update sets the provided resources to a running task
func (t *proxyTask) Update(ctx context.Context, any *types.Any) error {
	_, err := t.rt.Update(ctx, &rtapi.UpdateTaskRequest{
		ID:        t.tid,
		Resources: any,
	})
	return err
}

// Process returns a process within the task for the provided id
func (t *proxyTask) Process(ctx context.Context, execid string) (runtime.Process, error) {
	p := newProxyProcess(t.rt, t.tid, execid)
	_, err := p.State(ctx)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Stats returns runtime specific metrics for a task
func (t *proxyTask) Stats(ctx context.Context) (*types.Any, error) {
	resp, err := t.rt.Stats(ctx, &rtapi.StatsRequest{
		ID: t.tid,
	})
	if err != nil {
		return nil, err
	}
	return resp.Stats, nil
}

func newProxyProcess(rt rtapi.PlatformRuntimeClient, tid string, execid string) runtime.Process {
	return &proxyProcess{
		rt:     rt,
		tid:    tid,
		execid: execid,
	}
}

type proxyProcess struct {
	tid    string
	execid string
	rt     rtapi.PlatformRuntimeClient
}

// ID of the process
func (p *proxyProcess) ID() string {
	return p.execid
}

// State returns the process state
func (p *proxyProcess) State(ctx context.Context) (runtime.State, error) {
	resp, err := p.rt.State(ctx, &rtapi.StateRequest{
		ID:     p.tid,
		ExecID: p.execid,
	})
	if err != nil {
		return runtime.State{}, nil
	}
	return runtime.State{
		Status:     runtime.Status(resp.Status),
		Pid:        resp.Pid,
		ExitStatus: resp.ExitStatus,
		ExitedAt:   resp.ExitedAt,
		Stdin:      resp.Stdin,
		Stdout:     resp.Stdout,
		Stderr:     resp.Stderr,
		Terminal:   resp.Terminal,
	}, nil
}

// Kill signals a container
func (p *proxyProcess) Kill(ctx context.Context, signal uint32, all bool) error {
	_, err := p.rt.Kill(ctx, &rtapi.KillRequest{
		ID:     p.tid,
		ExecID: p.tid,
		Signal: signal,
		All:    all,
	})
	return err
}

// Pty resizes the processes pty/console
func (p *proxyProcess) ResizePty(ctx context.Context, size runtime.ConsoleSize) error {
	_, err := p.rt.ResizePty(ctx, &rtapi.ResizePtyRequest{
		ID:     p.tid,
		ExecID: p.execid,
		Width:  size.Width,
		Height: size.Height,
	})
	return err
}

// CloseStdin closes the processes stdin
func (p *proxyProcess) CloseIO(ctx context.Context) error {
	_, err := p.rt.CloseIO(ctx, &rtapi.CloseIORequest{
		ID:     p.tid,
		ExecID: p.execid,
	})
	return err
}

// Start the container's user defined process
func (p *proxyProcess) Start(ctx context.Context) error {
	_, err := p.rt.Start(ctx, &rtapi.StartRequest{
		ID:     p.tid,
		ExecID: p.execid,
	})
	return err
}

// Wait for the process to exit
func (p *proxyProcess) Wait(ctx context.Context) (*runtime.Exit, error) {
	resp, err := p.rt.Wait(ctx, &rtapi.WaitRequest{
		ID:     p.tid,
		ExecID: p.tid,
	})
	if err != nil {
		return nil, err
	}
	// Process don't return pid
	return &runtime.Exit{
		Status:    resp.ExitStatus,
		Timestamp: resp.ExitedAt,
	}, nil
}

// Delete deletes the process
func (p *proxyProcess) Delete(ctx context.Context) (*runtime.Exit, error) {
	resp, err := p.rt.Delete(ctx, &rtapi.DeleteRequest{
		ID:     p.tid,
		ExecID: p.execid,
	})
	if err != nil {
		return nil, err
	}
	return &runtime.Exit{
		Pid:       resp.Pid,
		Status:    resp.ExitStatus,
		Timestamp: resp.ExitedAt,
	}, nil
}
