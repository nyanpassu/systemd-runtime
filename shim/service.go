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
	"os"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/containerd/cgroups"
	cgroupsv2 "github.com/containerd/cgroups/v2"
	eventstypes "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/pkg/process"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/containerd/runtime/v2/runc"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/containerd/sys"
	goRunc "github.com/containerd/go-runc"
	"github.com/containerd/typeurl"
	ptypes "github.com/gogo/protobuf/types"
)

var (
	empty = &ptypes.Empty{}
)

func (s *Service) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, err error) {
	return nil, errdefs.ErrNotImplemented
}

// Start a process
func (s *Service) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{})
	if err != nil {
		return nil, err
	}
	defer release()

	sender := s.sender.PrepareSendL()
	defer sender.Cancel()

	p, err := container.Start(ctx, r)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	switch r.ExecID {
	case "":
		switch cg := container.Cgroup().(type) {
		case cgroups.Cgroup:
			if err := s.ep.Add(container.ID, cg); err != nil {
				logrus.WithError(err).Error("add cg to OOM monitor")
			}
		case *cgroupsv2.Manager:
			allControllers, err := cg.RootControllers()
			if err != nil {
				logrus.WithError(err).Error("failed to get root controllers")
			} else {
				if err := cg.ToggleControllers(allControllers, cgroupsv2.Enable); err != nil {
					if sys.RunningInUserNS() {
						logrus.WithError(err).Debugf("failed to enable controllers (%v)", allControllers)
					} else {
						logrus.WithError(err).Errorf("failed to enable controllers (%v)", allControllers)
					}
				}
			}
			if err := s.ep.Add(container.ID, cg); err != nil {
				logrus.WithError(err).Error("add cg to OOM monitor")
			}
		}

		sender.SendTaskStart(&eventstypes.TaskStart{
			ContainerID: container.ID,
			Pid:         uint32(p.Pid()),
		})
	default:
		sender.SendExecStart(&eventstypes.TaskExecStarted{
			ContainerID: container.ID,
			ExecID:      r.ExecID,
			Pid:         uint32(p.Pid()),
		})
	}
	return &taskAPI.StartResponse{
		Pid: uint32(p.Pid()),
	}, nil
}

// Delete the initial process and container
func (s *Service) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (_ *taskAPI.DeleteResponse, err error) {

	// if we are deleting an init task, make the holder as deleting
	if r.ExecID != "" {
		container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
		if err != nil {
			return nil, errdefs.ToGRPC(err)
		}
		defer release()
		return s.performDelete(ctx, container, r)
	}

	container, release, cancel, err := s.containerHolder.GetLockedContainerForDelete(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, errdefs.ToGRPC(ErrContainerDeleted)
	}
	defer func() {
		if err != nil {
			cancel()
			return
		}
		release()
	}()
	return s.performDelete(ctx, container, r)
}

// Exec an additional process inside the container
func (s *Service) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})

	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	defer release()

	ok, cancel := container.ReserveProcess(r.ExecID)
	if !ok {
		return nil, errdefs.ToGRPCf(errdefs.ErrAlreadyExists, "id %s", r.ExecID)
	}
	process, err := container.Exec(ctx, r)
	if err != nil {
		cancel()
		return nil, errdefs.ToGRPC(err)
	}

	s.sender.SendEventExecAdded(&eventstypes.TaskExecAdded{
		ContainerID: container.ID,
		ExecID:      process.ID(),
	})
	return empty, nil
}

// ResizePty of a process
func (s *Service) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	defer release()
	if err := container.ResizePty(ctx, r); err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return empty, nil
}

// State returns runtime state information for a process
func (s *Service) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		log.G(ctx).WithError(err).Error("getContainer error")
		return nil, errdefs.ToGRPC(err)
	}
	defer release()

	p, err := container.Process(r.ExecID)
	if err != nil {
		log.G(ctx).WithError(err).Error("getProcess error")
		return nil, errdefs.ToGRPC(err)
	}
	st, err := p.Status(ctx)
	if err != nil {
		log.G(ctx).WithError(err).Error("getStatus error")
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

	resp := &taskAPI.StateResponse{
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
	}
	return resp, nil
}

// Pause the container
func (s *Service) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	defer release()

	if err := container.Pause(ctx); err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	s.sender.SendEventPaused(&eventstypes.TaskPaused{
		ContainerID: container.ID,
	})
	return empty, nil
}

// Resume the container
func (s *Service) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, err
	}
	defer release()

	if err := container.Resume(ctx); err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	s.sender.SendEventResumed(&eventstypes.TaskResumed{
		ContainerID: container.ID,
	})
	return empty, nil
}

// Kill a process with the provided signal
func (s *Service) Kill(ctx context.Context, r *taskAPI.KillRequest) (_ *ptypes.Empty, err error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, err
	}
	defer release()

	done, cancel := s.status.Kill()
	defer func() {
		if err != nil {
			cancel()
			return
		}
		done()
	}()
	if err := container.Kill(ctx, r); err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return empty, nil
}

// Pids returns all pids inside the container
func (s *Service) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, err
	}
	defer release()

	pids, err := s.getContainerPids(ctx, r.ID)
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
	return &taskAPI.PidsResponse{
		Processes: processes,
	}, nil
}

// CloseIO of a process
func (s *Service) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, err
	}
	defer release()

	if err := container.CloseIO(ctx, r); err != nil {
		return nil, err
	}
	return empty, nil
}

// Checkpoint the container
func (s *Service) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
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
func (s *Service) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, err
	}
	defer release()

	if err := container.Update(ctx, r); err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return empty, nil
}

// Wait for a process to exit
func (s *Service) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, err
	}
	defer release()

	p, err := container.Process(r.ExecID)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	p.Wait()

	return &taskAPI.WaitResponse{
		ExitStatus: uint32(p.ExitStatus()),
		ExitedAt:   p.ExitedAt(),
	}, nil
}

// Connect returns shim information such as the shim's pid
func (s *Service) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	defer release()

	return &taskAPI.ConnectResponse{
		ShimPid: uint32(os.Getpid()),
		TaskPid: uint32(container.Pid()),
	}, nil
}

func (s *Service) Shutdown(ctx context.Context, _ *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	log.G(ctx).Info("shutdown")
	defer log.G(ctx).Info("shutdown done")

	deleted := s.containerHolder.IsDeleted()

	// return out if the shim is still serving containers
	if !deleted {
		return empty, nil
	}
	log.G(ctx).Info("cancel")

	s.sender.Close()

	if s.platform != nil {
		log.G(ctx).Info("close platform")
		s.platform.Close()
	}
	if s.shimAddress != "" {
		log.G(ctx).Info("remove socket")
		_ = RemoveSocket(s.shimAddress)
	}
	return empty, nil
}

func (s *Service) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: r.ID})
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
	data, err := typeurl.MarshalAny(statsx)
	if err != nil {
		return nil, err
	}
	return &taskAPI.StatsResponse{
		Stats: data,
	}, nil
}

// Delete the initial process and container
func (s *Service) performDelete(ctx context.Context, container *runc.Container, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	p, err := container.Delete(ctx, r)
	if err != nil {
		return nil, err
	}
	return &taskAPI.DeleteResponse{
		ExitStatus: uint32(p.ExitStatus()),
		ExitedAt:   p.ExitedAt(),
		Pid:        uint32(p.Pid()),
	}, nil
}

func (s *Service) processExits() {
	for e := range s.ec {
		s.checkProcesses(e)
	}
}

func (s *Service) shutdownAsync(ctx context.Context) {
	log.G(ctx).Info("processes are all done, shutdown async")
	go func() {

		container, release, cancel, err := s.containerHolder.GetLockedContainerForDelete(GetContainerOption{})
		if err != nil {
			if err == ErrContainerDeleted {
				log.G(ctx).Warn("container has already deleted")
			} else if err == ErrContainerNotCreated {
				log.G(ctx).Warn("container not created")
			} else {
				log.G(ctx).WithError(err).Error("get container for delete error")
			}
			return
		}

		resp, err := s.performDelete(ctx, container, &taskAPI.DeleteRequest{
			ID: container.ID,
		})
		if err != nil {
			log.G(ctx).WithError(err).Error("delete container error")
			cancel()
			return
		}
		log.G(ctx).WithField(
			"resp.Pid", resp.Pid,
		).WithField(
			"resp.ExitStatus", resp.ExitStatus,
		).WithField(
			"resp.ExitedAt", resp.ExitedAt,
		).Info(
			"container killed",
		)
		exit := runtime.Exit{
			Pid:       resp.Pid,
			Status:    resp.ExitStatus,
			Timestamp: resp.ExitedAt,
		}
		if err = s.writeExitStatus(ctx, &exit); err != nil {
			log.G(ctx).WithError(err).Error("write exit status error")
		}

		log.G(ctx).Info("kill container success")
		release()

		if _, err := s.Shutdown(ctx, nil); err != nil {
			log.G(ctx).WithError(err).Error("shutdown error")
		}
	}()
}

func (s *Service) writeExitStatus(ctx context.Context, exit *runtime.Exit) error {
	status, err := s.statusManager.LockForUpdateStatus(ctx)
	if err != nil {
		return err
	}
	status.Exit = exit
	defer func() {
		if err := s.statusManager.UnlockStatusFile(); err != nil {
			log.G(ctx).WithError(err).Error("unlock status file error")
		}
	}()
	return s.statusManager.UpdateStatus(ctx, status)
}

func (s *Service) checkProcesses(e goRunc.Exit) {
	ctx := context.Background()
	log.G(ctx).Info("check processes")
	defer log.G(ctx).Info("done check processes")

	livingProcessesCount := 0
	defer func() {
		if s.status.HasStarted() && livingProcessesCount == 0 {
			s.shutdownAsync(context.Background())
		}
	}()

	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{})
	if err != nil {
		log.G(ctx).WithError(err).Error("")
		return
	}
	defer release()

	if !container.HasPid(e.Pid) {
		livingProcessesCount += len(container.All())
		return
	}

	for _, p := range container.All() {
		if p.Pid() != e.Pid {
			livingProcessesCount++
			continue
		}

		if ip, ok := p.(*process.Init); ok {
			// Ensure all children are killed
			if runc.ShouldKillAllOnExit(ctx, container.Bundle) {
				if err := ip.KillAll(ctx); err != nil {
					logrus.WithError(err).WithField("id", ip.ID()).
						Error("failed to kill init's children")
				}
			}
		}

		p.SetExited(e.Status)

		if e.Pid != container.Pid() {
			s.sender.SendEventExit(&eventstypes.TaskExit{
				ContainerID: container.ID,
				ID:          p.ID(),
				Pid:         uint32(e.Pid),
				ExitStatus:  uint32(e.Status),
				ExitedAt:    p.ExitedAt(),
			})
			return
		}
		s.sender.SendEventContainerExit(&eventstypes.TaskExit{
			ContainerID: container.ID,
			ID:          p.ID(),
			Pid:         0,
			ExitStatus:  uint32(e.Status),
			ExitedAt:    p.ExitedAt(),
		}, &s.status)
	}
}

func (s *Service) getContainerPids(ctx context.Context, id string) ([]uint32, error) {
	container, release, err := s.containerHolder.GetLockedContainer(GetContainerOption{ID: id})
	if err != nil {
		return nil, err
	}
	defer release()

	p, err := container.Process("")
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	ps, err := p.(*process.Init).Runtime().Ps(ctx, id)
	if err != nil {
		return nil, err
	}
	pids := make([]uint32, 0, len(ps))
	for _, pid := range ps {
		pids = append(pids, uint32(pid))
	}
	return pids, nil
}

// initialize a single epoll fd to manage our consoles. `initPlatform` should
// only be called once.
func (s *Service) initPlatform() error {
	if s.platform != nil {
		return nil
	}
	p, err := runc.NewPlatform()
	if err != nil {
		return err
	}
	s.platform = p
	return nil
}

func (s *Service) createContainer(ctx context.Context, opts runtime.CreateOpts) (evt *eventstypes.TaskCreate, err error) {
	topts := opts.TaskOptions
	if topts == nil {
		topts = opts.RuntimeOptions
	}
	request := &taskAPI.CreateTaskRequest{
		ID:     s.id,
		Bundle: s.bundlePath,
		// Stdin:      opts.IO.Stdin,
		// Stdout:     opts.IO.Stdout,
		// Stderr:     opts.IO.Stderr,
		// Terminal:   opts.IO.Terminal,
		Checkpoint: opts.Checkpoint,
		Options:    topts,
	}
	for _, m := range opts.Rootfs {
		request.Rootfs = append(request.Rootfs, &types.Mount{
			Type:    m.Type,
			Source:  m.Source,
			Options: m.Options,
		})
	}
	if _, err = s.containerHolder.NewContainer(func() (*runc.Container, error) {
		container, err := runc.NewContainer(ctx, s.platform, request)
		if err != nil {
			return nil, err
		}
		return container, nil
	}); err != nil {
		s.logger.WithError(err).Error("create new container error")
	}

	return &eventstypes.TaskCreate{
		ContainerID: request.ID,
		Bundle:      request.Bundle,
		Rootfs:      request.Rootfs,
		IO: &eventstypes.TaskIO{
			Stdin:    request.Stdin,
			Stdout:   request.Stdout,
			Stderr:   request.Stderr,
			Terminal: request.Terminal,
		},
		Checkpoint: request.Checkpoint,
		// we send a pid 0 to represent init process pid
		// because after container restart we will get a different pid to report exit
		// moby will compare pid to conclude container exit
		Pid: uint32(0),
	}, err
}
