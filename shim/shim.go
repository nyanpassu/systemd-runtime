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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/events"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/containerd/cgroups"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/oom"
	oomv1 "github.com/containerd/containerd/pkg/oom/v1"
	oomv2 "github.com/containerd/containerd/pkg/oom/v2"
	"github.com/containerd/containerd/pkg/process"
	"github.com/containerd/containerd/pkg/stdio"
	"github.com/containerd/containerd/runtime/v2/runc"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/containerd/sys/reaper"
	goRunc "github.com/containerd/go-runc"

	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"

	"github.com/projecteru2/systemd-runtime/common"
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
	// socket
	socket         string
	socketListener net.Listener
	noSetupLogger  bool
	debug          bool
	logger         *logrus.Entry

	platform stdio.Platform
	ec       chan goRunc.Exit
	ep       oom.Watcher

	statusManager   *common.StatusManager
	status          SyncedServiceStatus
	sender          *EventSender
	containerHolder ContainerHolder

	shimAddress string
}

type CreateShimOpts struct {
	ID             string
	BundlePath     string
	Publisher      Publisher
	Debug          bool
	NoSetupLogger  bool
	Socket         string
	SocketListener net.Listener
}

// returns a new shim service that can be used via GRPC
func NewShimService(ctx context.Context, opts CreateShimOpts) (*Service, error) {
	var (
		ep        oom.Watcher
		err       error
		namespace string
	)
	namespace, _ = namespaces.Namespace(ctx)
	if cgroups.Mode() == cgroups.Unified {
		ep, err = oomv2.New(opts.Publisher)
	} else {
		ep, err = oomv1.New(opts.Publisher)
	}
	if err != nil {
		return nil, err
	}
	go ep.Run(ctx)

	sender := NewEventSender()
	sender.SetPublisher(namespace, opts.Publisher)

	statusManager, err := common.NewStatusManager(opts.BundlePath, log.G(ctx))
	if err != nil {
		return nil, err
	}
	s := &Service{
		id:             opts.ID,
		bundlePath:     opts.BundlePath,
		debug:          opts.Debug,
		noSetupLogger:  opts.NoSetupLogger,
		socket:         opts.Socket,
		socketListener: opts.SocketListener,

		ec:            reaper.Default.Subscribe(),
		ep:            ep,
		status:        SyncedServiceStatus{},
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

func (s *Service) InitContainer(ctx context.Context) (err error) {
	opts, err := common.LoadOpts(ctx, s.bundlePath)
	if err != nil {
		return err
	}

	if _, err := s.Cleanup(ctx); err != nil {
		logrus.WithError(err).Error("Cleanup error")
		return err
	}

	defer func() {
		if err != nil {
			if err := s.statusManager.ReleaseLocks(); err != nil {
				log.G(ctx).WithError(err).Error("release bundle file locks error")
			}
		}
	}()

	status, err := s.statusManager.LockForStartShim(ctx)
	if err != nil {
		return err
	}

	evt, err := s.createContainer(ctx, opts)
	if err != nil {
		return err
	}

	if !status.Created {
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

	if _, err = s.Start(ctx, &shimapi.StartRequest{
		ID: s.id,
	}); err != nil {
		return err
	}
	return nil
}

func (s *Service) Cleanup(ctx context.Context) (*shimapi.DeleteResponse, error) {
	log.G(ctx).WithField("id", s.id).Info("Begin Cleanup")
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(cwd), s.id)
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	runtime := "/usr/bin/runc"

	logrus.WithField("id", s.id).Info("ReadOptions")
	opts, err := runc.ReadOptions(path)
	if err != nil {
		return nil, err
	}
	root := process.RuncRoot
	if opts != nil && opts.Root != "" {
		root = opts.Root
	}

	logrus.WithField("runtime", runtime).WithField("ns", ns).WithField("root", root).WithField("path", path).Info("NewRunc")
	r := newRunc(root, path, ns, runtime, "", false)

	logrus.WithField("id", s.id).Info("Runc Delete")
	if err := r.Delete(ctx, s.id, &goRunc.DeleteOpts{
		Force: true,
	}); err != nil {
		logrus.WithError(err).Warn("failed to remove runc container")
	}
	logrus.Info("UnmountAll")
	if err := mount.UnmountAll(filepath.Join(path, "rootfs"), 0); err != nil {
		logrus.WithError(err).Warn("failed to cleanup rootfs mount")
	}
	logrus.Info("Cleanup complete")

	return &shimapi.DeleteResponse{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

func (s *Service) Serve(ctx context.Context) (err error) {
	if !s.noSetupLogger {
		if err := s.setLogger(ctx); err != nil {
			return err
		}
	}

	if err := s.serveTaskService(ctx); err != nil {
		if err != context.Canceled {
			return err
		}
	}

	_, err = s.Shutdown(context.Background(), &shimapi.ShutdownRequest{})
	return err
}

// Serve the shim server
func (s *Service) serveTaskService(ctx context.Context) error {
	logger := log.G(ctx)

	server, err := ttrpc.NewServer(ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()))
	if err != nil {
		return errors.Wrap(err, "failed creating server")
	}

	logger.Info("registering ttrpc server")
	shimapi.RegisterTaskService(server, s)

	if err := s.serveTTRPC(ctx, server); err != nil {
		logger.Error("serve error")
		return err
	}
	s.setupDumpStacks(logger)

	<-ctx.Done()
	return ctx.Err()
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
		TimestampFormat: log.RFC3339NanoFixed,
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
func (s *Service) serveTTRPC(ctx context.Context, server *ttrpc.Server) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	go func() {
		for {
			// send address over fifo
			_ = common.SendAddressOverFifo(context.Background(), wd, s.socket)
		}
	}()

	var (
		l net.Listener
	)
	if s.socketListener == nil {
		l, err = s.serveListener()
		if err != nil {
			logrus.Errorf("serveListener failed, path = %s", s.socket)
			return err
		}
	} else {
		l = s.socketListener
	}

	go func() {
		if err := server.Serve(ctx, l); err != nil &&
			!strings.Contains(err.Error(), "use of closed network connection") {
			logrus.WithError(err).Fatal("containerd-shim: ttrpc server failure")
		}
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
