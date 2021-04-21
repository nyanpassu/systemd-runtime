package proxy

import (
	"context"
	"net"

	pbtypes "github.com/gogo/protobuf/types"
	"google.golang.org/grpc"

	rtapi "github.com/containerd/containerd/api/services/runtime/v1"
	apitypes "github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/runtime"
)

var empty = &pbtypes.Empty{}

func RunPlatformRuntimeService(runtime runtime.PlatformRuntime) error {
	listener, err := net.Listen("unix", "")
	if err != nil {
		return err
	}

	server := grpc.NewServer()
	rtapi.RegisterPlatformRuntimeServer(server, &taskServer{})
	return server.Serve(listener)
}

type taskServer struct {
	runtime runtime.PlatformRuntime
}

// Create a task.
func (ts *taskServer) Create(ctx context.Context, r *rtapi.CreateTaskRequest) (*rtapi.CreateTaskResponse, error) {
	task, err := ts.runtime.Create(ctx, r.ID, runtime.CreateOpts{
		Spec: nil,
		// Rootfs mounts to perform to gain access to the container's filesystem
		Rootfs: nil,
		// IO for the container's main process
		IO: runtime.IO{
			Stdin:    r.Stdin,
			Stdout:   r.Stdout,
			Stderr:   r.Stderr,
			Terminal: r.Terminal,
		},
		// Checkpoint digest to restore container state
		Checkpoint: "",
		// RuntimeOptions for the runtime
		RuntimeOptions: nil,
		// TaskOptions received for the task
		TaskOptions: nil,
	})
	if err != nil {
		return nil, err
	}
	return &rtapi.CreateTaskResponse{
		Pid: task.PID(),
	}, nil
}

// Start a process.
func (ts *taskServer) Start(ctx context.Context, r *rtapi.StartRequest) (*rtapi.StartResponse, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if err := task.Start(ctx); err != nil {
		return nil, err
	}
	return &rtapi.StartResponse{}, nil
}

// Delete a task and on disk state.
func (ts *taskServer) Delete(ctx context.Context, r *rtapi.DeleteRequest) (*rtapi.DeleteResponse, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	exit, err := task.Delete(ctx)
	if err != nil {
		return nil, err
	}
	return &rtapi.DeleteResponse{
		Pid:        exit.Pid,
		ExitStatus: exit.Status,
		ExitedAt:   exit.Timestamp,
	}, nil
}

func (ts *taskServer) Get(ctx context.Context, r *rtapi.GetTaskRequest) (*rtapi.GetTaskResponse, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	state, err := task.State(ctx)
	if err != nil {
		return nil, err
	}
	return &rtapi.GetTaskResponse{
		Task: &rtapi.TaskProcess{
			Namespace: task.Namespace(),
			Process: &apitypes.Process{
				ID:         task.ID(),
				Pid:        state.Pid,
				Status:     apitypes.Status(state.Status),
				Stdin:      state.Stdin,
				Stdout:     state.Stdout,
				Stderr:     state.Stderr,
				Terminal:   state.Terminal,
				ExitStatus: state.ExitStatus,
				ExitedAt:   state.ExitedAt,
			},
		},
	}, nil
}

func (ts *taskServer) State(ctx context.Context, r *rtapi.StateRequest) (*rtapi.StateResponse, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	getState := func(state runtime.State, err error) (*rtapi.StateResponse, error) {
		if err != nil {
			return nil, err
		}
		return &rtapi.StateResponse{
			Status:     apitypes.Status(state.Status),
			Pid:        state.Pid,
			ExitStatus: state.ExitStatus,
			ExitedAt:   state.ExitedAt,
			Stdin:      state.Stdin,
			Stdout:     state.Stdout,
			Stderr:     state.Stderr,
			Terminal:   state.Terminal,
		}, nil
	}
	if r.ExecID == "" {
		return getState(task.State(ctx))
	}
	proc, err := task.Process(ctx, r.ExecID)
	if err != nil {
		return nil, err
	}
	return getState(proc.State(ctx))
}

func (ts *taskServer) List(ctx context.Context, r *rtapi.ListTasksRequest) (*rtapi.ListTasksResponse, error) {
	tasks, err := ts.runtime.Tasks(ctx, false)
	if err != nil {
		return nil, err
	}
	var processes []*rtapi.TaskProcess
	for _, task := range tasks {
		status, err := task.State(ctx)
		if err != nil {
			return nil, err
		}
		processes = append(processes, &rtapi.TaskProcess{
			Namespace: task.Namespace(),
			Process: &apitypes.Process{
				ID:         task.ID(),
				Pid:        status.Pid,
				Status:     apitypes.Status(status.Status),
				Stdin:      status.Stdin,
				Stdout:     status.Stdout,
				Stderr:     status.Stderr,
				Terminal:   status.Terminal,
				ExitStatus: status.ExitStatus,
				ExitedAt:   status.ExitedAt,
			},
		})
	}
	return &rtapi.ListTasksResponse{
		Tasks: processes,
	}, nil
}

// Kill a task or process.
func (ts *taskServer) Kill(ctx context.Context, r *rtapi.KillRequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if r.All {
		if err = task.Kill(ctx, r.Signal, true); err != nil {
			return nil, err
		}
		return empty, nil
	}
	proc, err := task.Process(ctx, r.ExecID)
	if err != nil {
		return nil, err
	}
	if err := proc.Kill(ctx, r.Signal, false); err != nil {
		return nil, err
	}
	return empty, nil
}

func (ts *taskServer) Exec(ctx context.Context, r *rtapi.ExecProcessRequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if _, err = task.Exec(ctx, r.ExecID, runtime.ExecOpts{
		Spec: r.Spec,
		IO: runtime.IO{
			Stdin:    r.Stdin,
			Stdout:   r.Stdout,
			Stderr:   r.Stderr,
			Terminal: r.Terminal,
		},
	}); err != nil {
		return nil, err
	}
	return empty, nil
}

func (ts *taskServer) Stats(ctx context.Context, r *rtapi.StatsRequest) (*rtapi.StatsResponse, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	stat, err := task.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return &rtapi.StatsResponse{
		Stats: stat,
	}, nil
}

func (ts *taskServer) ResizePty(ctx context.Context, r *rtapi.ResizePtyRequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	resize := func(f func(runtime.ConsoleSize) error) (*pbtypes.Empty, error) {
		if err := f(runtime.ConsoleSize{
			Width:  r.Width,
			Height: r.Height,
		}); err != nil {
			return nil, err
		}
		return empty, nil
	}
	if r.ExecID == "" {
		return resize(func(size runtime.ConsoleSize) error { return task.ResizePty(ctx, size) })
	}
	process, err := task.Process(ctx, r.ExecID)
	if err != nil {
		return nil, err
	}
	return resize(func(size runtime.ConsoleSize) error { return process.ResizePty(ctx, size) })
}

func (ts *taskServer) CloseIO(ctx context.Context, r *rtapi.CloseIORequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	close := func(f func() error) (*pbtypes.Empty, error) {
		if err := f(); err != nil {
			return nil, err
		}
		return empty, nil
	}
	if r.ExecID != "" {
		return close(func() error { return task.CloseIO(ctx) })
	}
	proc, err := task.Process(ctx, r.ExecID)
	if err != nil {
		return nil, err
	}
	return close(func() error { return proc.CloseIO(ctx) })
}

func (ts *taskServer) Pause(ctx context.Context, r *rtapi.PauseTaskRequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if err := task.Pause(ctx); err != nil {
		return nil, err
	}
	return empty, nil
}

func (ts *taskServer) Resume(ctx context.Context, r *rtapi.ResumeTaskRequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if err := task.Resume(ctx); err != nil {
		return nil, err
	}
	return empty, nil
}

func (ts *taskServer) Pids(ctx context.Context, r *rtapi.PidsRequest) (*rtapi.PidsResponse, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	processes, err := task.Pids(ctx)
	if err != nil {
		return nil, err
	}
	var infos []*apitypes.ProcessInfo
	for _, proc := range processes {
		infos = append(infos, &apitypes.ProcessInfo{
			Pid: proc.Pid,
			// TODO
			// Info: proc.Info,
		})
	}
	return &rtapi.PidsResponse{
		Processes: infos,
	}, nil
}

func (ts *taskServer) Checkpoint(ctx context.Context, r *rtapi.CheckpointTaskRequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if err = task.Checkpoint(ctx, r.Path, r.Options); err != nil {
		return nil, err
	}
	return empty, nil
}

func (ts *taskServer) Update(ctx context.Context, r *rtapi.UpdateTaskRequest) (*pbtypes.Empty, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if err = task.Update(ctx, r.Resources); err != nil {
		return nil, err
	}
	return empty, nil
}

func (ts *taskServer) Wait(ctx context.Context, r *rtapi.WaitRequest) (*rtapi.WaitResponse, error) {
	task, err := ts.runtime.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	wait := func(exit *runtime.Exit, err error) (*rtapi.WaitResponse, error) {
		if err != nil {
			return nil, err
		}
		return &rtapi.WaitResponse{
			ExitStatus: exit.Status,
			ExitedAt:   exit.Timestamp,
		}, nil
	}
	if r.ExecID == "" {
		return wait(task.Wait(ctx))
	}
	proc, err := task.Process(ctx, r.ExecID)
	if err != nil {
		return nil, err
	}
	return wait(proc.Wait(ctx))
}
