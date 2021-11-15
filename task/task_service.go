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
	// LoadTimeout .
	LoadTimeout = "io.containerd.timeout.shim.load"
	// CleanupTimeout .
	CleanupTimeout = "io.containerd.timeout.shim.cleanup"
	// ShutdownTimeout .
	ShutdownTimeout = "io.containerd.timeout.shim.shutdown"
)

//nolint:gochecknoinits
func init() {
	timeout.Set(LoadTimeout, 5*time.Second)
	timeout.Set(CleanupTimeout, 5*time.Second)
	timeout.Set(ShutdownTimeout, 3*time.Second)
}

type Service struct {
	taskPid     uint32
	id          string
	namespace   string
	taskService taskv2.TaskService
}

// ID of bundle, same as container
func (s *Service) ID() string {
	return s.id
}

// State returns the process state
func (s *Service) State(ctx context.Context) (state runtime.State, err error) {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::State")
	defer func() {
		var status string
		switch state.Status {
		case runtime.CreatedStatus:
			status = "created"
		case runtime.RunningStatus:
			status = "running"
		case runtime.StoppedStatus:
			status = "stopped"
		case runtime.PausedStatus:
			status = "paused"
		case runtime.PausingStatus:
			status = "pausing"
		default:
			status = "unknown"
		}
		if err == nil {
			log.G(ctx).WithField("id", s.ID()).Debugf("[TaskService State] state = %v", status)
		}
	}()

	response, err := s.taskService.State(ctx, &taskv2.StateRequest{
		ID: s.ID(),
	})
	if err != nil {
		log.G(ctx).WithField("id", s.ID()).WithError(err).Error("State error")
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
		Pid: 0,
		// Pid:        response.Pid,
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
func (s *Service) Kill(ctx context.Context, signal uint32, all bool) error {
	log.G(ctx).WithField(
		"id", s.ID(),
	).WithField(
		"signal", signal,
	).WithField(
		"all", all,
	).Debug("call TaskService::Kill")
	if _, err := s.taskService.Kill(ctx, &taskv2.KillRequest{
		ID:     s.ID(),
		Signal: signal,
		All:    all,
	}); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// ResizePty resizes the processes pty/console
func (s *Service) ResizePty(ctx context.Context, size runtime.ConsoleSize) error {
	_, err := s.taskService.ResizePty(ctx, &taskv2.ResizePtyRequest{
		ID:     s.ID(),
		Width:  size.Width,
		Height: size.Height,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// CloseIO closes the processes io
func (s *Service) CloseIO(ctx context.Context) error {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::CloseIO")
	_, err := s.taskService.CloseIO(ctx, &taskv2.CloseIORequest{
		ID:    s.ID(),
		Stdin: true,
	})
	if err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Start the container's user defined process
func (s *Service) Start(ctx context.Context) error {
	log.G(ctx).WithField("id", s.ID()).Error("should not call TaskService::Start, service will start by itself")
	return errdefs.ErrNotImplemented
}

// Wait for the process to exit
func (s *Service) Wait(ctx context.Context) (exit *runtime.Exit, err error) {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Wait")

	response, err := s.taskService.Wait(ctx, &taskv2.WaitRequest{
		ID: s.ID(),
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return &runtime.Exit{
		Pid:       s.taskPid,
		Timestamp: response.ExitedAt,
		Status:    response.ExitStatus,
	}, nil
}

// Delete deletes the process
func (s *Service) Delete(ctx context.Context) (_ *runtime.Exit, err error) {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Delete")
	response, shimErr := s.taskService.Delete(ctx, &taskv2.DeleteRequest{
		ID: s.ID(),
	})
	if shimErr != nil {
		log.G(ctx).WithField("id", s.ID()).WithError(shimErr).Debug("failed to delete task")
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
func (s *Service) PID() uint32 {
	return s.taskPid
}

// Namespace that the task exists in
func (s *Service) Namespace() string {
	return s.namespace
}

// Exec adds a process into the container
func (s *Service) Exec(ctx context.Context, id string, opts runtime.ExecOpts) (runtime.Process, error) {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Exec")

	if err := identifiers.Validate(id); err != nil {
		return nil, errors.Wrapf(err, "invalid exec id %s", id)
	}
	request := &taskv2.ExecProcessRequest{
		ID:       s.ID(),
		ExecID:   id,
		Stdin:    opts.IO.Stdin,
		Stdout:   opts.IO.Stdout,
		Stderr:   opts.IO.Stderr,
		Terminal: opts.IO.Terminal,
		Spec:     opts.Spec,
	}
	if _, err := s.taskService.Exec(ctx, request); err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return &process{
		id:   id,
		task: s,
	}, nil
}

// Pids returns all pids
func (s *Service) Pids(ctx context.Context) ([]runtime.ProcessInfo, error) {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Pids")
	resp, err := s.taskService.Pids(ctx, &taskv2.PidsRequest{
		ID: s.ID(),
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
func (s *Service) Checkpoint(ctx context.Context, path string, options *types.Any) error {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Checkpoint")
	request := &taskv2.CheckpointTaskRequest{
		ID:      s.ID(),
		Path:    path,
		Options: options,
	}
	if _, err := s.taskService.Checkpoint(ctx, request); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Update sets the provided resources to a running task
func (s *Service) Update(ctx context.Context, resources *types.Any, _ map[string]string) error {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Update")
	if _, err := s.taskService.Update(ctx, &taskv2.UpdateTaskRequest{
		ID:        s.ID(),
		Resources: resources,
	}); err != nil {
		return errdefs.FromGRPC(err)
	}
	return nil
}

// Process returns a process within the task for the provided id
func (s *Service) Process(ctx context.Context, execid string) (runtime.Process, error) {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Process")
	p := &process{
		id:   execid,
		task: s,
	}
	if _, err := p.State(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// Stats returns runtime specific metrics for a task
func (s *Service) Stats(ctx context.Context) (*types.Any, error) {
	log.G(ctx).WithField("id", s.ID()).Debug("call TaskService::Stats")
	response, err := s.taskService.Stats(ctx, &taskv2.StatsRequest{
		ID: s.ID(),
	})
	if err != nil {
		return nil, errdefs.FromGRPC(err)
	}
	return response.Stats, nil
}
