package task

import (
	"context"

	"github.com/containerd/ttrpc"
	"github.com/pkg/errors"

	tasktypes "github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"

	taskv2 "github.com/containerd/containerd/runtime/v2/task"
)

type process struct {
	id   string
	task *Service
}

func (p *process) ID() string {
	return p.id
}

func (p *process) Kill(ctx context.Context, signal uint32, _ bool) error {
	log.G(ctx).WithField("id", p.task.ID()).WithField("pid", p.ID()).Debug("call TaskService::Kill")
	_, err := p.task.taskService.Kill(ctx, &taskv2.KillRequest{
		Signal: signal,
		ID:     p.task.ID(),
		ExecID: p.id,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

func (p *process) State(ctx context.Context) (runtime.State, error) {
	log.G(ctx).WithField("id", p.task.ID()).WithField("pid", p.ID()).Debug("call TaskService::State")
	response, err := p.task.taskService.State(ctx, &taskv2.StateRequest{
		ID:     p.task.ID(),
		ExecID: p.id,
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

// ResizePty changes the side of the process's PTY to the provided width and height
func (p *process) ResizePty(ctx context.Context, size runtime.ConsoleSize) error {
	log.G(ctx).WithField("id", p.task.ID()).WithField("pid", p.ID()).Debug("call TaskService::ResizePty")
	_, err := p.task.taskService.ResizePty(ctx, &taskv2.ResizePtyRequest{
		ID:     p.task.ID(),
		ExecID: p.id,
		Width:  size.Width,
		Height: size.Height,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// CloseIO closes the provided IO pipe for the process
func (p *process) CloseIO(ctx context.Context) error {
	log.G(ctx).WithField("id", p.task.ID()).WithField("pid", p.ID()).Debug("call TaskService::CloseIO")
	_, err := p.task.taskService.CloseIO(ctx, &taskv2.CloseIORequest{
		ID:     p.task.ID(),
		ExecID: p.id,
		Stdin:  true,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Start the process
func (p *process) Start(ctx context.Context) error {
	log.G(ctx).WithField("id", p.task.ID()).WithField("pid", p.ID()).Debug("call TaskService::Start")
	_, err := p.task.taskService.Start(ctx, &taskv2.StartRequest{
		ID:     p.task.ID(),
		ExecID: p.id,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Wait on the process to exit and return the exit status and timestamp
func (p *process) Wait(ctx context.Context) (*runtime.Exit, error) {
	log.G(ctx).WithField("id", p.task.ID()).WithField("pid", p.ID()).Debug("call TaskService::Wait")
	response, err := p.task.taskService.Wait(ctx, &taskv2.WaitRequest{
		ID:     p.task.ID(),
		ExecID: p.id,
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return &runtime.Exit{
		Timestamp: response.ExitedAt,
		Status:    response.ExitStatus,
	}, nil
}

func (p *process) Delete(ctx context.Context) (*runtime.Exit, error) {
	response, err := p.task.taskService.Delete(ctx, &taskv2.DeleteRequest{
		ID:     p.task.ID(),
		ExecID: p.id,
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return &runtime.Exit{
		Status:    response.ExitStatus,
		Timestamp: response.ExitedAt,
		Pid:       response.Pid,
	}, nil
}
