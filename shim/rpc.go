//go:build linux
// +build linux

/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package shim

import (
	"context"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/pkg/process"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/typeurl"
	ptypes "github.com/gogo/protobuf/types"
)

var (
	empty = &ptypes.Empty{}
)

// ShimTaskService will use this type to dispatch rpc request
type ShimTaskService struct {
	service ShimService
}

func (s *ShimTaskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, err error) {
	return nil, errdefs.ErrNotImplemented
}

// Start a process
func (s *ShimTaskService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	if r.ExecID == "" {
		return nil, errdefs.ErrNotImplemented
	}

	pid, err := s.service.Start(ctx, r.ExecID)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return &taskAPI.StartResponse{
		Pid: pid,
	}, nil
}

// Delete the initial process and container
func (s *ShimTaskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (_ *taskAPI.DeleteResponse, err error) {
	exitStatus, exitedAt, pid, err := s.service.Delete(ctx, r.ExecID)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return &taskAPI.DeleteResponse{
		Pid:        pid,
		ExitStatus: exitStatus,
		ExitedAt:   exitedAt,
	}, nil
}

// Exec an additional process inside the container
func (s *ShimTaskService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	execConfig := &process.ExecConfig{
		ID:       r.ExecID,
		Terminal: r.Terminal,
		Stdin:    r.Stdin,
		Stdout:   r.Stdout,
		Stderr:   r.Stderr,
		Spec:     r.Spec,
	}
	if err := s.service.Exec(ctx, execConfig); err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return empty, nil
}

// ResizePty of a process
func (s *ShimTaskService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	if err := s.service.ResizePty(ctx, r.ExecID, uint16(r.Width), uint16(r.Height)); err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return empty, nil
}

// State returns runtime state information for a process
func (s *ShimTaskService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	state, err := s.service.State(ctx, r.ExecID)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return &taskAPI.StateResponse{
		ID:         state.ID,
		Bundle:     state.Bundle,
		Pid:        state.Pid,
		Status:     state.Status,
		Stdin:      state.Stdin,
		Stdout:     state.Stdout,
		Stderr:     state.Stderr,
		Terminal:   state.Terminal,
		ExitStatus: state.ExitStatus,
		ExitedAt:   state.ExitedAt,
	}, nil
}

// Pause the container
func (s *ShimTaskService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

// Resume the container
func (s *ShimTaskService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

// Kill a process with the provided signal
func (s *ShimTaskService) Kill(ctx context.Context, r *taskAPI.KillRequest) (_ *ptypes.Empty, err error) {
	if err := s.service.Kill(ctx, r.ExecID, r.Signal, r.All); err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return empty, nil
}

// Pids returns all pids inside the container
func (s *ShimTaskService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	processes, err := s.service.Pids(ctx)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return &taskAPI.PidsResponse{
		Processes: processes,
	}, nil
}

// CloseIO of a process
func (s *ShimTaskService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	if err := s.service.CloseIO(ctx, r.ExecID); err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return empty, nil
}

// Checkpoint the container
func (s *ShimTaskService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	var opts options.CheckpointOptions
	if r.Options != nil {
		v, err := typeurl.UnmarshalAny(r.Options)
		if err != nil {
			return empty, errdefs.ToGRPC(err)
		}
		opts = *v.(*options.CheckpointOptions)
	}
	return s.service.Checkpoint(ctx, &process.CheckpointConfig{
		Path:                     r.Path,
		Exit:                     opts.Exit,
		AllowOpenTCP:             opts.OpenTcp,
		AllowExternalUnixSockets: opts.ExternalUnixSockets,
		AllowTerminal:            opts.Terminal,
		FileLocks:                opts.FileLocks,
		EmptyNamespaces:          opts.EmptyNamespaces,
		WorkDir:                  opts.WorkPath,
	})
}

// Update a running container
func (s *ShimTaskService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	if err := s.service.Update(ctx, r.Resources); err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return empty, nil
}

// Wait for a process to exit
func (s *ShimTaskService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	exitStatus, exitAt, err := s.service.Wait(ctx, r.ExecID)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return &taskAPI.WaitResponse{
		ExitStatus: exitStatus,
		ExitedAt:   exitAt,
	}, nil
}

// Connect returns shim information such as the shim's pid
func (s *ShimTaskService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	shimPid, taskPid, err := s.service.Connect(ctx)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return &taskAPI.ConnectResponse{
		ShimPid: shimPid,
		TaskPid: taskPid,
	}, nil
}

func (s *ShimTaskService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	if err := s.service.Shutdown(ctx); err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return empty, nil
}

func (s *ShimTaskService) Stats(ctx context.Context, _ *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	data, err := s.service.Stats(ctx)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	return &taskAPI.StatsResponse{
		Stats: data,
	}, nil
}
