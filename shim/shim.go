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
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/containerd/containerd/events"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/containerd/cgroups"
	cgroupsv2 "github.com/containerd/cgroups/v2"
	eventstypes "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/api/types/task"
	clog "github.com/containerd/containerd/log"
	ns "github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/oom"
	oomv1 "github.com/containerd/containerd/pkg/oom/v1"
	oomv2 "github.com/containerd/containerd/pkg/oom/v2"
	"github.com/containerd/containerd/pkg/process"
	"github.com/containerd/containerd/pkg/stdio"
	cruntime "github.com/containerd/containerd/runtime"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/containerd/sys"
	"github.com/containerd/containerd/sys/reaper"
	goRunc "github.com/containerd/go-runc"

	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/runc"
)

// Publisher for events
type Publisher interface {
	events.Publisher
	io.Closer
}

// service is the shim implementation of a remote shim over GRPC
type Service struct {
	// id of the task
	id string
	// bundle path of shim running on
	bundlePath string
	// namespace
	namespace      string
	socket         string
	socketListener net.Listener
	noSetupLogger  bool
	debug          bool
	logger         *logrus.Entry
	ctx            context.Context
	cancel         func()

	platform stdio.Platform
	ec       chan goRunc.Exit
	ep       oom.Watcher

	statusManager   *common.StatusManager
	sender          *EventSender
	containerHolder ContainerHolder

	shimStarted      uint32
	shimDisabled     uint32
	shimFirstStarted uint32
	exitStatus       atomic.Value

	shimAddress string
}

type CreateShimOpts struct {
	ID             string
	BundlePath     string
	Publisher      Publisher
	Logger         *logrus.Entry
	Debug          bool
	NoSetupLogger  bool
	Socket         string
	SocketListener net.Listener
}

type Status struct {
	ID         string
	ExecID     string
	Bundle     string
	Pid        uint32
	Status     task.Status
	Stdin      string
	Stdout     string
	Stderr     string
	Terminal   bool
	ExitStatus uint32
	ExitedAt   time.Time
}

// returns a new shim service that can be used via GRPC
func NewShimService(ctx context.Context, opts CreateShimOpts) (s *Service, err error) {
	var (
		ep        oom.Watcher
		namespace string
	)
	namespace, _ = ns.Namespace(ctx)
	if cgroups.Mode() == cgroups.Unified {
		ep, err = oomv2.New(opts.Publisher)
	} else {
		ep, err = oomv1.New(opts.Publisher)
	}
	if err != nil {
		return nil, err
	}
	go ep.Run(ctx)

	statusManager, err := common.NewStatusManager(opts.BundlePath, opts.Logger)
	if err != nil {
		return nil, err
	}

	sender := NewEventSender(statusManager)
	sender.SetPublisher(namespace, opts.Publisher)

	serviceCtx, cancel := context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			cancel()
		}
	}()
	s = &Service{
		id:             opts.ID,
		bundlePath:     opts.BundlePath,
		debug:          opts.Debug,
		noSetupLogger:  opts.NoSetupLogger,
		socket:         opts.Socket,
		socketListener: opts.SocketListener,
		logger:         opts.Logger,
		ctx:            serviceCtx,
		cancel:         cancel,

		ec:            reaper.Default.Subscribe(),
		ep:            ep,
		sender:        sender,
		statusManager: statusManager,
	}
	go s.processExits()

	goRunc.Monitor = reaper.Default
	if err := s.initPlatform(); err != nil {
		return nil, errors.Wrap(err, "failed to initialized platform behavior")
	}
	if address, err := ReadAddress("address"); err == nil {
		s.shimAddress = address
	}
	return s, nil
}

func (s *Service) Cleanup(ctx context.Context, cleanBundle bool) (*shimapi.DeleteResponse, error) {
	return common.Cleanup(ctx, s.id, s.namespace, s.bundlePath, cleanBundle, s.logger)
}

func (s *Service) Done() <-chan struct{} {
	return s.ctx.Done()
}

func (s *Service) Serve(ctx context.Context, sigChan <-chan uint32) (err error) {
	s.logger.Debug("[Service Serve]")
	if err := s.init(ctx); err != nil {
		s.logger.WithError(err).Error("[Service Serve] init failed")
		return err
	}

	if !s.noSetupLogger {
		ctx, cancel := context.WithCancel(clog.WithLogger(context.Background(), s.logger))
		defer cancel()

		if err := s.setLogger(ctx); err != nil {
			return err
		}
	}

	_, err = s.serveTaskService(s.logger)
	if err != nil {
		return err
	}

	// go func() {
	// 	<-s.Done()
	// 	if err := shutdown(context.Background()); err != nil {
	// 		s.logger.WithError(err).Error("shutdown ttrpc service error")
	// 	}
	// }()

	go func() {
		for sig := range sigChan {
			if err := s.killContainer(ns.WithNamespace(context.Background(), s.namespace), sig); err != nil {
				s.logger.WithError(err).Error("kill container error")
			}
		}
	}()

	return nil
}

func (s *Service) init(ctx context.Context) (err error) {
	opts, err := common.LoadOpts(ctx, s.bundlePath)
	if err != nil {
		return err
	}

	status, err := s.statusManager.LockForStartShim(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			if err := s.statusManager.ReleaseLocks(); err != nil {
				s.logger.WithError(err).Error("release bundle file locks error")
			}
		}
	}()

	if _, err := s.Cleanup(ctx, false); err != nil {
		logrus.WithError(err).Error("Cleanup error")
		return err
	}

	evt, err := s.createContainer(ctx, opts)
	if err != nil {
		s.logger.WithError(err).Error("[Service init] create container failed")
		return err
	}

	if !status.Created {
		s.setFirstStarted()
		s.sender.SendEventCreate(evt)
	}

	status.PID = os.Getpid()
	status.Created = true
	if err := s.statusManager.UpdateStatus(ctx, status); err != nil {
		return err
	}
	if err := s.statusManager.UnlockStatusFile(); err != nil {
		return err
	}

	if _, err = s.start(ctx); err != nil {
		s.logger.WithError(err).Error("start failed")
		return err
	}
	return nil
}

// Serve the shim server
func (s *Service) serveTaskService(logger *logrus.Entry) (func(context.Context) error, error) {
	server, err := ttrpc.NewServer(ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()))
	if err != nil {
		return nil, errors.Wrap(err, "failed creating server")
	}

	logger.Debug("registering ttrpc server")
	shimapi.RegisterTaskService(server, &ShimTaskService{service: s, logger: logger})

	if err := s.serveTTRPC(logger, server); err != nil {
		logger.Error("serve error")
		return nil, err
	}
	s.setupDumpStacks(logger)

	return server.Shutdown, nil
}

func (s *Service) setupDumpStacks(logger *logrus.Entry) {
	dump := make(chan os.Signal, 32)
	signal.Notify(dump, syscall.SIGUSR1)

	go func() {
		for range dump {
			s.dumpStacks(logger)
		}
	}()
}

func (s *Service) setLogger(ctx context.Context) error {
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: clog.RFC3339NanoFixed,
		FullTimestamp:   true,
	})
	if s.debug {
		logrus.SetLevel(logrus.DebugLevel)
	}
	f, err := s.openLog(ctx)
	if err != nil {
		return err
	}
	logrus.SetOutput(f)
	return nil
}

func (s *Service) openLog(ctx context.Context) (io.Writer, error) {
	return fifo.OpenFifoDup2(ctx, "log", unix.O_WRONLY, 0700, int(os.Stderr.Fd()))
}

// serve serves the ttrpc API over a unix socket at the provided path
// this function does not block
func (s *Service) serveTTRPC(logger *logrus.Entry, server *ttrpc.Server) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	closed := new(int32)

	go func() {
		for {
			// send address over fifo
			if atomic.LoadInt32(closed) == 1 {
				return
			}
			_ = common.SendAddressOverFifo(context.Background(), wd, s.socket)
		}
	}()

	var (
		l net.Listener
	)
	if s.socketListener == nil {
		l, err = s.serveListener()
		if err != nil {
			logger.Errorf("serveListener failed, path = %s", s.socket)
			return err
		}
	} else {
		l = s.socketListener
	}

	go func() {
		if err := server.Serve(context.Background(), l); err != nil &&
			!strings.Contains(err.Error(), "use of closed network connection") {
			logrus.WithError(err).Fatal("containerd-shim: ttrpc server failure")
		}

		atomic.StoreInt32(closed, 1)

		l.Close()
		if address, err := ReadAddress("address"); err == nil {
			_ = RemoveSocket(address)
		}
	}()
	return nil
}

func (s *Service) dumpStacks(logger *logrus.Entry) {
	var (
		buf       []byte
		stackSize int
	)
	bufferLen := 16384
	for stackSize == len(buf) {
		buf = make([]byte, bufferLen)
		stackSize = runtime.Stack(buf, true)
		bufferLen *= 2
	}
	buf = buf[:stackSize]
	logger.Infof("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===", buf)
}

func (s *Service) serveListener() (net.Listener, error) {
	var (
		l    net.Listener
		err  error
		path string = s.socket
	)
	if path == "" {
		l, err = net.FileListener(os.NewFile(3, "socket"))
		path = "[inherited from parent]"
	} else {
		if len(path) > socketPathLimit {
			return nil, errors.Errorf("%q: unix socket path too long (> %d)", path, socketPathLimit)
		}
		l, err = net.Listen("unix", path)
	}
	if err != nil {
		return nil, err
	}
	logrus.WithField("socket", path).Debug("serving api on socket")
	return l, nil
}

func (s *Service) processExits() {
	for e := range s.ec {
		s.checkProcesses(context.Background(), e)
	}
}

func (s *Service) checkProcesses(ctx context.Context, e goRunc.Exit) {
	s.logger.Info("[Service checkProcesses]")
	livingProcessesCount := 0
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		if err == ErrContainerNotCreated {
			s.logger.Debug("check processes when container is not created")
		}
		if err == ErrContainerDeleted {
			s.logger.Warn("check processes when container is deleted")
		}
		return
	}
	defer release()

	if container == nil {
		s.logger.Warn("container not created")
		return
	}

	if !container.HasPid(e.Pid) {
		s.logger.Debugf("container hasn't pid %v", e.Pid)
		livingProcessesCount += len(container.All())
		return
	}

	for _, p := range container.All() {
		if !s.checkProcess(ctx, container, p, e) {
			livingProcessesCount++
		}
	}
	started := s.started()
	s.logger.Infof("check processes, started = %b, livingProcessesCount = %d", started, livingProcessesCount)
	if started && livingProcessesCount == 0 {
		go s.cleanup(context.Background())
	}
}

func (s *Service) checkProcess(ctx context.Context, container *runc.Container, p process.Process, e goRunc.Exit) (matched bool) {
	if p.Pid() != e.Pid {
		s.logger.Debugf("process is not what we are looking for %v", e.Pid)
		return false
	}
	matched = true

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
		s.logger.Info("SendEventExit")
		s.sender.SendEventExit(&eventstypes.TaskExit{
			ContainerID: container.ID,
			ID:          p.ID(),
			Pid:         uint32(e.Pid),
			ExitStatus:  uint32(e.Status),
			ExitedAt:    p.ExitedAt(),
		})
		return
	}

	if s.Disabled() {
		s.logger.Info("SendEventContainerExit")
		s.sender.SendEventExit(&eventstypes.TaskExit{
			ContainerID: container.ID,
			ID:          p.ID(),
			Pid:         0,
			ExitStatus:  uint32(e.Status),
			ExitedAt:    p.ExitedAt(),
		})
		return
	}

	s.logger.Info("SendEventPaused")
	s.sender.SendEventPaused(&eventstypes.TaskPaused{
		ContainerID: container.ID,
	})
	return
}

func (s *Service) cleanup(ctx context.Context) {
	container, release, cancel, err := s.containerHolder.GetLockedContainerForDelete()
	if err != nil {
		if err == ErrContainerDeleted {
			s.logger.Warn("container has already deleted")
		} else if err == ErrContainerNotCreated {
			s.logger.Warn("container not created")
		} else {
			s.logger.WithError(err).Error("get container for delete error")
		}
		return
	}

	exitStatus, exitedAt, pid, err := s.performDelete(ctx, container, "")
	if err != nil {
		s.logger.WithError(err).Error("delete container error")
		cancel()
		return
	}
	defer release()

	s.logger.WithField("resp.Pid", pid).WithField("resp.ExitStatus", exitStatus).WithField("resp.ExitedAt", exitedAt).Info("container killed")
	exit := cruntime.Exit{
		Pid:       pid,
		Status:    exitStatus,
		Timestamp: exitedAt,
	}
	if err = s.writeExitStatus(ctx, &exit); err != nil {
		s.logger.WithError(err).Error("write exit status error")
	}

	s.logger.Info("kill container success")
	if !s.Disabled() {
		s.close()
	}
}

func (s *Service) close() {
	if s.platform != nil {
		s.logger.Info("close platform")
		s.platform.Close()
	}
	if s.shimAddress != "" {
		s.logger.Info("remove socket")
		_ = RemoveSocket(s.shimAddress)
	}
	s.sender.Close()
	s.cancel()
}

func (s *Service) writeExitStatus(ctx context.Context, exit *cruntime.Exit) error {
	s.exitStatus.Store(*exit)

	status, err := s.statusManager.LockForUpdateStatus(ctx)
	if err != nil {
		return err
	}
	status.Exit = exit
	defer func() {
		if err := s.statusManager.UnlockStatusFile(); err != nil {
			s.logger.WithError(err).Error("unlock status file error")
		}
	}()
	return s.statusManager.UpdateStatus(ctx, status)
}

func (s *Service) createContainer(ctx context.Context, opts cruntime.CreateOpts) (evt *eventstypes.TaskCreate, err error) {
	topts := opts.TaskOptions
	if topts == nil {
		topts = opts.RuntimeOptions
	}
	request := &shimapi.CreateTaskRequest{
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

func (s *Service) start(ctx context.Context) (pid uint32, err error) {
	container, release, err := s.containerHolder.GetLockedContainer()
	if err != nil {
		return pid, err
	}
	defer release()

	sender := s.sender.PrepareSendL()
	defer sender.Cancel()

	_, err = container.Start(ctx, "")
	if err != nil {
		return pid, err
	}

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

	s.setStarted()
	if s.firstStarted() {
		sender.SendTaskStart(&eventstypes.TaskStart{
			ContainerID: container.ID,
			Pid:         0,
		})
		return 0, nil
	}

	s.sender.SendEventResumed(&eventstypes.TaskResumed{
		ContainerID: s.id,
	})
	return 0, nil
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

func (s *Service) setDisabled() {
	atomic.StoreUint32(&s.shimDisabled, 1)
}

func (s *Service) Disabled() bool {
	return atomic.LoadUint32(&s.shimDisabled) == 1
}

func (s *Service) setStarted() {
	atomic.StoreUint32(&s.shimStarted, 1)
}

func (s *Service) started() bool {
	return atomic.LoadUint32(&s.shimStarted) == 1
}

func (s *Service) setFirstStarted() {
	atomic.StoreUint32(&s.shimFirstStarted, 1)
}

func (s *Service) firstStarted() bool {
	return atomic.LoadUint32(&s.shimFirstStarted) == 1
}
