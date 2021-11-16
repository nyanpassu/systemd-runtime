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
	"os"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/containerd/cgroups"
	cgroupsv2 "github.com/containerd/cgroups/v2"
	eventstypes "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/pkg/process"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	"github.com/containerd/typeurl"
	ptypes "github.com/gogo/protobuf/types"

	"github.com/projecteru2/systemd-runtime/runc"
)

// ShimService .
type ShimService interface { //nolint:revive
	Start(ctx context.Context, execID string) (pid uint32, err error)
	Delete(ctx context.Context, execID string) (exitStatus uint32, exitAt time.Time, pid uint32, err error)
	Exec(ctx context.Context, config *process.ExecConfig) error
	ResizePty(ctx context.Context, execID string, width uint16, height uint16) (err error)
	State(ctx context.Context, execID string) (*Status, error)
	Kill(ctx context.Context, execID string, signal uint32, all bool) (err error)
	Pids(ctx context.Context) ([]*task.ProcessInfo, error)
	CloseIO(ctx context.Context, execID string) (err error)
	Checkpoint(ctx context.Context, r *process.CheckpointConfig) (*ptypes.Empty, error)
	Update(ctx context.Context, resources *ptypes.Any) (err error)
	Wait(ctx context.Context, execID string) (exitStatus uint32, exitAt time.Time, err error)
	Connect(ctx context.Context) (shimPid uint32, taskPid uint32, err error)
	Shutdown(ctx context.Context) (err error)
	Stats(ctx context.Context) (*ptypes.Any, error)
}

// Start a process
func (s *Service) Start(ctx context.Context, execID string) (pid uint32, err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return pid, err
	}
	defer release()

	sender := s.sender.PrepareSendL()
	defer sender.Cancel()

	p, err := container.Start(ctx, execID)
	if err != nil {
		return pid, err
	}

	pid = uint32(p.Pid())
	sender.SendExecStart(&eventstypes.TaskExecStarted{
		ContainerID: container.ID,
		ExecID:      execID,
		Pid:         pid,
	})
	return pid, nil
}

// Delete the initial process and container
func (s *Service) Delete(ctx context.Context, execID string) (exitStatus uint32, exitAt time.Time, pid uint32, err error) {
	if execID != "" {
		container, release, err := s.containerHolder.GetLockedContainer()
		if err != nil {
			return exitStatus, exitAt, pid, err
		}
		defer release()

		return s.performDelete(ctx, container, execID)
	}

	container, release, cancel, err := s.containerHolder.GetLockedContainerForDelete()
	if err == ErrContainerDeleted {
		defer s.close()
		exitStatus := s.exitStatus.Load().(runtime.Exit)
		return exitStatus.Status, exitStatus.Timestamp, exitStatus.Pid, nil
	}
	if err != nil {
		return exitStatus, exitAt, pid, err
	}
	defer func() {
		if err != nil {
			cancel()
			return
		}
		release()
	}()
	return s.performDelete(ctx, container, execID)
}

// Exec an additional process inside the container
func (s *Service) Exec(ctx context.Context, config *process.ExecConfig) error {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return err
	}
	defer release()

	ok, cancel := container.ReserveProcess(config.ID)
	if !ok {
		return errdefs.ToGRPCf(errdefs.ErrAlreadyExists, "id %s", config.ID)
	}
	process, err := container.Exec(ctx, config)
	if err != nil {
		cancel()
		return err
	}

	s.sender.SendEventExecAdded(&eventstypes.TaskExecAdded{
		ContainerID: container.ID,
		ExecID:      process.ID(),
	})
	return nil
}

// ResizePty of a process
func (s *Service) ResizePty(ctx context.Context, execID string, width uint16, height uint16) (err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return err
	}
	defer release()

	return container.ResizePty(ctx, execID, width, height)
}

// State returns runtime state information for a process
func (s *Service) State(ctx context.Context, execID string) (*Status, error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err == ErrContainerDeleted && execID == "" {
		exitStatus := s.exitStatus.Load().(runtime.Exit)
		return &Status{
			ID:         s.id,
			Bundle:     s.bundlePath,
			Status:     task.StatusStopped,
			ExitStatus: exitStatus.Status,
			ExitedAt:   exitStatus.Timestamp,
		}, nil
	}
	if err != nil {
		s.logger.WithError(err).Error("getContainer error")
		return nil, errdefs.ToGRPC(err)
	}
	defer release()

	p, err := container.Process(execID)
	if err != nil {
		s.logger.WithError(err).Error("getProcess error")
		return nil, errdefs.ToGRPC(err)
	}
	st, err := p.Status(ctx)
	if err != nil {
		s.logger.WithError(err).Error("getStatus error")
		return nil, errdefs.ToGRPC(err)
	}
	status := task.StatusUnknown
	switch st {
	case "created":
		status = task.StatusCreated
	case "running":
		status = task.StatusRunning
	case "stopped":
		status = task.StatusStopped
	case "paused":
		status = task.StatusPaused
	case "pausing":
		status = task.StatusPausing
	}
	sio := p.Stdio()

	return &Status{
		ID:         p.ID(),
		Bundle:     container.Bundle,
		Pid:        uint32(p.Pid()),
		Status:     status,
		Stdin:      sio.Stdin,
		Stdout:     sio.Stdout,
		Stderr:     sio.Stderr,
		Terminal:   sio.Terminal,
		ExitStatus: uint32(p.ExitStatus()),
		ExitedAt:   p.ExitedAt(),
	}, nil
}

// Kill a process with the provided signal
func (s *Service) Kill(ctx context.Context, execID string, signal uint32, all bool) (err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return err
	}
	defer release()

	if err := container.Kill(ctx, execID, signal, all); err != nil {
		return err
	}

	if (signal == uint32(unix.SIGTERM) || signal == uint32(unix.SIGKILL)) && execID == "" {
		s.setDisabled()
	}
	return nil
}

// Pids returns all pids inside the container
func (s *Service) Pids(ctx context.Context) ([]*task.ProcessInfo, error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return nil, err
	}
	defer release()

	pids, err := s.getContainerPids(ctx)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	var processes []*task.ProcessInfo
	for _, pid := range pids {
		pInfo := task.ProcessInfo{
			Pid: pid,
		}
		for _, p := range container.ExecdProcesses() {
			if p.Pid() == int(pid) {
				d := &options.ProcessDetails{
					ExecID: p.ID(),
				}
				a, err := typeurl.MarshalAny(d)
				if err != nil {
					return nil, errors.Wrapf(err, "failed to marshal process %d info", pid)
				}
				pInfo.Info = a
				break
			}
		}
		processes = append(processes, &pInfo)
	}
	return processes, nil
}

// CloseIO of a process
func (s *Service) CloseIO(ctx context.Context, execID string) (err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return err
	}
	defer release()

	return container.CloseIO(ctx, execID)
}

// Checkpoint the container
func (s *Service) Checkpoint(ctx context.Context, r *process.CheckpointConfig) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return nil, err
	}
	defer release()

	if err := container.Checkpoint(ctx, r); err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return empty, nil
}

// Update a running container
func (s *Service) Update(ctx context.Context, resources *ptypes.Any) (err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return err
	}
	defer release()

	return container.Update(ctx, resources)
}

// Wait for a process to exit
func (s *Service) Wait(ctx context.Context, execID string) (exitStatus uint32, exitAt time.Time, err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return exitStatus, exitAt, err
	}
	defer release()

	p, err := container.Process(execID)
	if err != nil {
		return exitStatus, exitAt, errdefs.ToGRPC(err)
	}
	p.Wait()

	return uint32(p.ExitStatus()), p.ExitedAt(), nil
}

// Connect returns shim information such as the shim's pid
func (s *Service) Connect(ctx context.Context) (shimPid uint32, taskPid uint32, err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return shimPid, taskPid, err
	}
	defer release()

	return uint32(os.Getpid()), uint32(container.Pid()), nil
}

func (s *Service) Shutdown(ctx context.Context) (err error) {
	// return out if the shim is still serving containers
	if !s.containerHolder.IsDeleted() {
		s.logger.Info("container is not deleted, cancel shutdown")
		return nil
	}

	s.close()
	return nil
}

func (s *Service) Stats(ctx context.Context) (*ptypes.Any, error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return nil, err
	}
	defer release()

	cgx := container.Cgroup()
	if cgx == nil {
		return nil, errdefs.ToGRPCf(errdefs.ErrNotFound, "cgroup does not exist")
	}
	var statsx interface{}
	switch cg := cgx.(type) {
	case cgroups.Cgroup:
		stats, err := cg.Stat(cgroups.IgnoreNotExist)
		if err != nil {
			return nil, err
		}
		statsx = stats
	case *cgroupsv2.Manager:
		stats, err := cg.Stat()
		if err != nil {
			return nil, err
		}
		statsx = stats
	default:
		return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "unsupported cgroup type %T", cg)
	}
	return typeurl.MarshalAny(statsx)
}

// Kill kill container, call from shim
func (s *Service) killContainer(ctx context.Context, signal uint32) (err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return err
	}
	defer release()

	s.logger.Debug("terminate shim")
	// defensive programming
	return container.Kill(ctx, "", signal, true)
}

// Delete the initial process and container
func (s *Service) performDelete(
	ctx context.Context,
	container *runc.Container,
	execID string,
) (exitStatus uint32, exitedAt time.Time, pid uint32, err error) {
	p, err := container.Delete(ctx, execID)
	if err != nil {
		return exitStatus, exitedAt, pid, err
	}
	return uint32(p.ExitStatus()), p.ExitedAt(), uint32(p.Pid()), nil
}

func (s *Service) getContainerPids(ctx context.Context) ([]uint32, error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return nil, err
	}
	defer release()

	p, err := container.Process("")
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	ps, err := p.(*process.Init).Runtime().Ps(ctx, s.id)
	if err != nil {
		return nil, err
	}
	pids := make([]uint32, 0, len(ps))
	for _, pid := range ps {
		pids = append(pids, uint32(pid))
	}
	return pids, nil
}
