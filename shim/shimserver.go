package shim

import (
	"context"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/log"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"golang.org/x/sys/unix"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/utils"
)

type ShimServer struct {
	Shim         *Shim
	Config       Config
	Publisher    *RemoteEventsPublisher
	SigsCh       chan interface{}
	shimLockFile *os.File
}

func (ss *ShimServer) start(ctx context.Context, createService CreateShimService) (ch <-chan error, err error) {
	service, err := ss.createService(ctx, createService)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			ss.unlockFile(ctx, ss.shimLockFile)
		}
	}()
	// now we create another context to serve the service
	return ss.serve(ss.Shim.makeContext(log.G(ctx)), service)
}

func (ss *ShimServer) createService(
	ctx context.Context,
	createService CreateShimService,
) (service ShimService, err error) {
	opts, err := common.LoadOpts(ctx, ss.Shim.bundlePath)
	if err != nil {
		return nil, err
	}

	if _, err := ss.Shim.cleanup(ctx); err != nil {
		logrus.WithError(err).Error("Cleanup error")
		return nil, err
	}

	statusFile, err := ss.getAndLockFile(ctx, func() (*os.File, error) {
		return common.OpenShimStatusFile(ss.Shim.bundlePath)
	})
	if err != nil {
		return nil, err
	}
	defer ss.unlockFile(ctx, statusFile)

	status := common.ShimStatus{}
	if _, err := utils.FileReadJSON(statusFile, &status); err != nil {
		return nil, err
	}
	if status.Disabled {
		return nil, common.ErrBundleDisabled
	}

	ss.shimLockFile, err = ss.getAndLockFile(ctx, func() (*os.File, error) {
		return common.OpenShimLockFile(ss.Shim.bundlePath)
	})
	if err != nil {
		return nil, err
	}

	shimCh := make(chan interface{}, 1)
	var cancel func()
	ctx, cancel = context.WithCancel(ctx)
	go func() {
		select {
		case <-ss.SigsCh:
		case <-shimCh:
		}
		cancel()
	}()

	service, err = createService(ctx, CreateShimOpts{
		ID:         ss.Shim.id,
		BundlePath: ss.Shim.bundlePath,
		CreateOpts: opts,
		Publisher:  ss.Publisher,
		Shutdown:   shimCh,
		Created:    status.Created,
	})
	if err != nil {
		return nil, err
	}

	status.PID = os.Getpid()
	status.Created = true
	if err := utils.FileWriteJSON(statusFile, &status); err != nil {
		ss.unlockFile(ctx, ss.shimLockFile)
		return nil, err
	}
	ss.unlockFile(ctx, statusFile)

	if _, err = service.Start(ctx, &shimapi.StartRequest{
		ID: ss.Shim.id,
	}); err != nil {
		return nil, err
	}
	return service, nil
}

func (ss *ShimServer) getAndLockFile(ctx context.Context, supplier func() (*os.File, error)) (*os.File, error) {
	file, err := supplier()
	if err != nil {
		return nil, err
	}
	if err := utils.FileLock(ctx, file); err != nil {
		utils.FileClose(file, log.G(ctx))
		return nil, err
	}
	return file, nil
}

func (ss *ShimServer) unlockFile(ctx context.Context, file *os.File) {
	if err := utils.FileUnlock(file); err != nil {
		log.G(ctx).WithError(err).Error("unlock status file error")
	}
}

func (ss *ShimServer) serve(ctx context.Context, service ShimService) (chan error, error) {
	if !ss.Config.NoSetupLogger {
		if err := ss.setLogger(ctx); err != nil {
			return nil, err
		}
	}

	if err := ss.serveTaskService(ctx, TaskService{ShimService: service}); err != nil {
		if err != context.Canceled {
			return nil, err
		}
	}

	ch := make(chan error)
	go func() {
		select {
		case <-ss.Publisher.Done():
			close(ch)
		case <-time.After(5 * time.Second):
			ch <- errors.New("publisher not closed")
			close(ch)
		}
	}()

	return ch, nil
}

// Serve the shim server
func (ss *ShimServer) serveTaskService(ctx context.Context, taskService shimapi.TaskService) error {
	logger := log.G(ctx)

	server, err := ttrpc.NewServer(ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()))
	if err != nil {
		return errors.Wrap(err, "failed creating server")
	}

	logger.Info("registering ttrpc server")
	shimapi.RegisterTaskService(server, taskService)

	if err := ss.serveTTRPC(ctx, server); err != nil {
		logger.Error("serve error")
		return err
	}
	ss.setupDumpStacks(logger)

	<-ctx.Done()
	return ctx.Err()
}

func (ss *ShimServer) setupDumpStacks(logger *logrus.Entry) {
	dump := make(chan os.Signal, 32)
	signal.Notify(dump, syscall.SIGUSR1)

	go func() {
		for range dump {
			ss.dumpStacks(logger)
		}
	}()
}

func (ss *ShimServer) setLogger(ctx context.Context) error {
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: log.RFC3339NanoFixed,
		FullTimestamp:   true,
	})
	if ss.Shim.debug {
		logrus.SetLevel(logrus.DebugLevel)
	}
	f, err := ss.openLog(ctx)
	if err != nil {
		return err
	}
	logrus.SetOutput(f)
	return nil
}

func (ss *ShimServer) openLog(ctx context.Context) (io.Writer, error) {
	return fifo.OpenFifoDup2(ctx, "log", unix.O_WRONLY, 0700, int(os.Stderr.Fd()))
}

// serve serves the ttrpc API over a unix socket at the provided path
// this function does not block
func (ss *ShimServer) serveTTRPC(ctx context.Context, server *ttrpc.Server) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	go func() {
		for {
			// send address over fifo
			_ = common.SendAddressOverFifo(context.Background(), wd, ss.Shim.socket)
		}
	}()

	var (
		l net.Listener
	)
	if ss.Shim.socketListener == nil {
		l, err = ss.serveListener()
		if err != nil {
			logrus.Errorf("serveListener failed, path = %s", ss.Shim.socket)
			return err
		}
	} else {
		l = ss.Shim.socketListener
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

func (ss *ShimServer) dumpStacks(logger *logrus.Entry) {
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

func (ss *ShimServer) serveListener() (net.Listener, error) {
	var (
		l    net.Listener
		err  error
		path string = ss.Shim.socket
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
