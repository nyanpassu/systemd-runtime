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
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/containerd/version"
	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/sys/reaper"
	"golang.org/x/sys/unix"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/utils"
)

const shimID = "io.containerd.systemd.v1"

// Client for a shim server
type Client struct {
	service shimapi.TaskService
	context context.Context
	// signals chan os.Signal
}

// Publisher for events
type Publisher interface {
	events.Publisher
	io.Closer
}

// Init func for the creation of a shim server
type Init func(context.Context, string, Publisher, func()) (Shim, error)

// Shim server interface
type Shim interface {
	shimapi.TaskService
	Cleanup(ctx context.Context, id string) (*shimapi.DeleteResponse, error)
	StartShim(ctx context.Context, opts StartOpts) (string, error)
	SystemdStartShim(ctx context.Context, opts StartOpts) (string, net.Listener, error)
}

// OptsKey is the context key for the Opts value.
type OptsKey struct{}

// Opts are context options associated with the shim invocation.
type Opts struct {
	BundlePath string
	Debug      bool
}

// StartOpts describes shim start configuration received from containerd
type StartOpts struct {
	ID               string
	ContainerdBinary string
	Address          string
	TTRPCAddress     string
}

// BinaryOpts allows the configuration of a shims binary setup
type BinaryOpts func(*Config)

// Config of shim binary options provided by shim implementations
type Config struct {
	// NoSubreaper disables setting the shim as a child subreaper
	NoSubreaper bool
	// NoReaper disables the shim binary from reaping any child process implicitly
	NoReaper bool
	// NoSetupLogger disables automatic configuration of logrus to use the shim FIFO
	NoSetupLogger bool
}

var (
	debugFlag            bool
	versionFlag          bool
	idFlag               string
	namespaceFlag        string
	socketFlag           string
	bundlePath           string
	addressFlag          string
	containerdBinaryFlag string
	action               string
	socketListener       net.Listener
)

const (
	ttrpcAddressEnv = "TTRPC_ADDRESS"
)

func parseFlags() {
	flag.BoolVar(&debugFlag, "debug", false, "enable debug output in logs")
	flag.BoolVar(&versionFlag, "v", false, "show the shim version and exit")
	flag.StringVar(&namespaceFlag, "namespace", "", "namespace that owns the shim")
	flag.StringVar(&idFlag, "id", "", "id of the task")
	flag.StringVar(&socketFlag, "socket", "", "socket path to serve")
	flag.StringVar(&bundlePath, "bundle", "", "path to the bundle if not workdir")

	flag.StringVar(&addressFlag, "address", "", "grpc address back to main containerd")
	flag.StringVar(&containerdBinaryFlag, "publish-binary", "containerd", "path to publish binary (used for publishing events)")

	flag.Parse()
	action = flag.Arg(0)
}

func setRuntime() {
	debug.SetGCPercent(40)
	go func() {
		for range time.Tick(30 * time.Second) {
			debug.FreeOSMemory()
		}
	}()
	if os.Getenv("GOMAXPROCS") == "" {
		// If GOMAXPROCS hasn't been set, we default to a value of 2 to reduce
		// the number of Go stacks present in the shim.
		runtime.GOMAXPROCS(2)
	}
}

// Run initializes and runs a shim server
func Run(opts ...BinaryOpts) {
	var config Config
	for _, o := range opts {
		o(&config)
	}
	if err := run(config); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", shimID, err)
		os.Exit(1)
	}
}

func run(config Config) error {
	parseFlags()
	if versionFlag {
		fmt.Printf("%s:\n", os.Args[0])
		fmt.Println("  Version: ", version.Version)
		fmt.Println("  Revision:", version.Revision)
		fmt.Println("  Go version:", version.GoVersion)
		fmt.Println("")
		return nil
	}

	setRuntime()

	signals, err := setupSignals(config)
	if err != nil {
		return err
	}
	if !config.NoSubreaper {
		if err := subreaper(); err != nil {
			return err
		}
	}

	if bundlePath == "" {
		bundlePath, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	ttrpcAddress := os.Getenv(ttrpcAddressEnv)
	publisher, err := NewPublisher(ttrpcAddress)
	if err != nil {
		return err
	}

	defer publisher.Close()

	if namespaceFlag == "" {
		return fmt.Errorf("shim namespace cannot be empty")
	}
	ctx := namespaces.WithNamespace(context.Background(), namespaceFlag)
	ctx = context.WithValue(ctx, OptsKey{}, Opts{BundlePath: bundlePath, Debug: debugFlag})
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("runtime", shimID))
	ctx, cancel := context.WithCancel(ctx)
	once := utils.NewOnce(cancel)

	disabled, err := common.BundleStarted(ctx, bundlePath)
	if err != nil {
		return err
	}
	if disabled {
		fmt.Println("bundle disabled, exit.")
		os.Exit(0)
		return nil
	}

	service, err := newShimService(ctx, idFlag, publisher, once.Run)
	if err != nil {
		return err
	}

	logger := logrus.WithFields(logrus.Fields{
		"pid":       os.Getpid(),
		"path":      bundlePath,
		"namespace": namespaceFlag,
	})
	go handleSignals(once.Run, logger, signals)

	switch action {
	case "delete":
		response, err := service.Cleanup(ctx, idFlag)
		if err != nil {
			return err
		}
		data, err := proto.Marshal(response)
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return err
		}
		return nil
	case "start":
		address, err := service.StartShim(ctx, StartOpts{
			ID:               idFlag,
			ContainerdBinary: containerdBinaryFlag,
			Address:          addressFlag,
			TTRPCAddress:     ttrpcAddress,
		})
		if err != nil {
			return err
		}
		if _, err := os.Stdout.WriteString(address); err != nil {
			return err
		}
		return nil
	case "systemd-start":
		logrus.WithField("id", idFlag).Info("SystemdStartShim")
		address, listener, err := service.SystemdStartShim(ctx, StartOpts{
			ID:               idFlag,
			ContainerdBinary: containerdBinaryFlag,
			Address:          addressFlag,
			TTRPCAddress:     ttrpcAddress,
		})
		if err != nil {
			return err
		}
		socketFlag = address
		socketListener = listener
		i := initializer{
			service:    service,
			id:         idFlag,
			bundlePath: bundlePath,
		}
		logrus.Info("Cleanup")
		_, err = service.Cleanup(ctx, idFlag)
		if err != nil {
			logrus.WithError(err).Error("Cleanup error")
			return err
		}
		go func() {
			initialize(namespaces.WithNamespace(context.Background(), namespaceFlag), i)
		}()
		return launch(ctx, config, service, publisher, logger)
	default:

		_, err = service.Cleanup(ctx, idFlag)
		if err != nil {
			return err
		}

		i := initializer{
			service:    service,
			id:         idFlag,
			bundlePath: bundlePath,
		}

		err = initialize(ctx, i)
		if err != nil {
			return err
		}
		return launch(ctx, config, service, publisher, logger)
	}
}

func initialize(ctx context.Context, i initializer) error {
	logrus.Info("initialize")
	if err := i.create(ctx); err != nil {
		logrus.WithError(err).Error("create failed")
		return err
	}
	logrus.Info("start")
	if err := i.start(ctx); err != nil {
		logrus.WithError(err).Error("start failed")
		return err
	}
	logrus.Info("initialize done")
	return nil
}

func launch(ctx context.Context, config Config, service shimapi.TaskService, publisher *RemoteEventsPublisher, logger *logrus.Entry) error {
	logrus.Info("Launch")
	if !config.NoSetupLogger {
		if err := setLogger(ctx, idFlag); err != nil {
			return err
		}
	}

	client := NewShimClient(ctx, service)
	if err := client.Serve(logger); err != nil {
		if err != context.Canceled {
			return err
		}
	}
	select {
	case <-publisher.Done():
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("publisher not closed")
	}
}

func setLogger(ctx context.Context, id string) error {
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: log.RFC3339NanoFixed,
		FullTimestamp:   true,
	})
	if debugFlag {
		logrus.SetLevel(logrus.DebugLevel)
	}
	f, err := openLog(ctx, id)
	if err != nil {
		return err
	}
	logrus.SetOutput(f)
	return nil
}

func openLog(ctx context.Context, _ string) (io.Writer, error) {
	return fifo.OpenFifoDup2(ctx, "log", unix.O_WRONLY, 0700, int(os.Stderr.Fd()))
}

// NewShimClient creates a new shim server client
func NewShimClient(ctx context.Context, svc shimapi.TaskService) *Client {
	s := &Client{
		service: svc,
		context: ctx,
	}
	return s
}

// Serve the shim server
func (s *Client) Serve(logger *logrus.Entry) error {
	dump := make(chan os.Signal, 32)
	setupDumpStacks(dump)

	server, err := newServer()
	if err != nil {
		return errors.Wrap(err, "failed creating server")
	}

	logrus.Info("registering ttrpc server")
	shimapi.RegisterTaskService(server, s.service)

	if err := serve(s.context, server, socketFlag); err != nil {
		logrus.Error("serve error")
		return err
	}
	go func() {
		for range dump {
			dumpStacks(logger)
		}
	}()
	<-s.context.Done()
	return s.context.Err()
}

// serve serves the ttrpc API over a unix socket at the provided path
// this function does not block
func serve(ctx context.Context, server *ttrpc.Server, path string) error {

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	go func() {
		for {
			// send address over fifo
			_ = common.SendAddressOverFifo(context.Background(), wd, path)
		}
	}()

	var (
		l net.Listener
	)
	if socketListener == nil {
		l, err = serveListener(path)
		if err != nil {
			logrus.Errorf("serveListener failed, path = %s", path)
			return err
		}
	} else {
		l = socketListener
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

func dumpStacks(logger *logrus.Entry) {
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

type initializer struct {
	service    shimapi.TaskService
	id         string
	bundlePath string
}

func (i *initializer) create(ctx context.Context) error {
	logrus.Info("LoadOpts")
	opts, err := common.LoadOpts(ctx, i.bundlePath)
	if err != nil {
		logrus.WithError(err).Error("LoadOpts failed")
		return err
	}

	topts := opts.TaskOptions
	if topts == nil {
		topts = opts.RuntimeOptions
	}
	request := &shimapi.CreateTaskRequest{
		ID:     i.id,
		Bundle: i.bundlePath,
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
	logrus.Info("Create")
	_, err = i.service.Create(ctx, request)
	if err != nil {
		return err
	}
	return nil
}

func (i *initializer) start(ctx context.Context) error {
	_, err := i.service.Start(ctx, &shimapi.StartRequest{
		ID: i.id,
	})
	return err
}

// setupSignals creates a new signal handler for all signals and sets the shim as a
// sub-reaper so that the container processes are reparented
func setupSignals(config Config) (chan os.Signal, error) {
	signals := make(chan os.Signal, 32)
	smp := []os.Signal{unix.SIGTERM, unix.SIGINT, unix.SIGPIPE}
	if !config.NoReaper {
		smp = append(smp, unix.SIGCHLD)
	}
	signal.Notify(signals, smp...)
	return signals, nil
}

func setupDumpStacks(dump chan<- os.Signal) {
	signal.Notify(dump, syscall.SIGUSR1)
}

func serveListener(path string) (net.Listener, error) {
	var (
		l   net.Listener
		err error
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

func handleSignals(cancel func(), logger *logrus.Entry, signals chan os.Signal) {
	logger.Info("starting signal loop")
	count := 0

	for sig := range signals {
		switch sig {
		case unix.SIGCHLD:
			if err := reaper.Reap(); err != nil {
				logger.WithError(err).Error("reap exit status")
			}
		case unix.SIGPIPE:
		case unix.SIGTERM:
			logger.Warn("sigal term")
			cancel()
		case unix.SIGINT:
			count++
			logger.Warn("sigal int")
			if count == 3 {
				logger.Warn("sigal int count > 3, terminating")
				cancel()
			}
		}
	}
}

func newServer() (*ttrpc.Server, error) {
	return ttrpc.NewServer(ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()))
}

func subreaper() error {
	return reaper.SetSubreaper(1)
}
