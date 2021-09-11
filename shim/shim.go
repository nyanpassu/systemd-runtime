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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/process"
	containerRuntime "github.com/containerd/containerd/runtime"
	"github.com/containerd/containerd/runtime/v2/runc"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/containerd/version"
	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"
	"github.com/gogo/protobuf/proto"
	types1 "github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/sys/reaper"
	"golang.org/x/sys/unix"

	"github.com/projecteru2/systemd-runtime/common"

	goRunc "github.com/containerd/go-runc"
	"github.com/containerd/typeurl"
	ptypes "github.com/gogo/protobuf/types"

	"github.com/containerd/cgroups"
	cgroupsv2 "github.com/containerd/cgroups/v2"
)

const shimID = "io.containerd.systemd.v1"

// group labels specifies how the shim groups services.
// currently supports a runc.v2 specific .group label and the
// standard k8s pod label.  Order matters in this list
var groupLabels = []string{
	"io.containerd.runc.v2.group",
	"io.kubernetes.cri.sandbox-id",
}

type spec struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Publisher for events
type Publisher interface {
	events.Publisher
	io.Closer
}

// Init func for the creation of a shim server
// type Init func(context.Context, string, Publisher, func()) (Shim, error)

type ShimService interface {
	Start(ctx context.Context, req *shimapi.StartRequest) (*shimapi.StartResponse, error)
	State(ctx context.Context, req *shimapi.StateRequest) (*shimapi.StateResponse, error)
	Delete(ctx context.Context, req *shimapi.DeleteRequest) (*shimapi.DeleteResponse, error)
	Pids(ctx context.Context, req *shimapi.PidsRequest) (*shimapi.PidsResponse, error)
	Pause(ctx context.Context, req *shimapi.PauseRequest) (*types1.Empty, error)
	Resume(ctx context.Context, req *shimapi.ResumeRequest) (*types1.Empty, error)
	Checkpoint(ctx context.Context, req *shimapi.CheckpointTaskRequest) (*types1.Empty, error)
	Kill(ctx context.Context, req *shimapi.KillRequest) (*types1.Empty, error)
	Exec(ctx context.Context, req *shimapi.ExecProcessRequest) (*types1.Empty, error)
	ResizePty(ctx context.Context, req *shimapi.ResizePtyRequest) (*types1.Empty, error)
	CloseIO(ctx context.Context, req *shimapi.CloseIORequest) (*types1.Empty, error)
	Update(ctx context.Context, req *shimapi.UpdateTaskRequest) (*types1.Empty, error)
	Wait(ctx context.Context, req *shimapi.WaitRequest) (*shimapi.WaitResponse, error)
	Stats(ctx context.Context, req *shimapi.StatsRequest) (*shimapi.StatsResponse, error)
	Connect(ctx context.Context, req *shimapi.ConnectRequest) (*shimapi.ConnectResponse, error)
	Shutdown(ctx context.Context, req *shimapi.ShutdownRequest) (*types1.Empty, error)
}

type TaskService struct {
	ShimService
}

func (t TaskService) Create(ctx context.Context, req *shimapi.CreateTaskRequest) (*shimapi.CreateTaskResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func taskService(shimService ShimService) shimapi.TaskService {
	return TaskService{
		ShimService: shimService,
	}
}

// OptsKey is the context key for the Opts value.
type OptsKey struct{}

// Opts are context options associated with the shim invocation.
type Opts struct {
	BundlePath string
	Debug      bool
}

type InitSocketOpts struct {
	ID      string
	Address string
}

// StartOpts describes shim start configuration received from containerd
type StartProcessOpts struct {
	InitSocketOpts
	ContainerdBinary string
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

type CreateShimOpts struct {
	ID         string
	BundlePath string
	Meta       Meta
	Publisher  Publisher
	CreateOpts containerRuntime.CreateOpts
	Shutdown   chan<- interface{}
}

type CreateShim func(context.Context, CreateShimOpts) (ShimService, error)

type Flags struct {
	debug            bool
	version          bool
	id               string
	namespace        string
	socket           string
	bundlePath       string
	address          string
	containerdBinary string
	action           string
	socketListener   net.Listener
}

var (
	flags Flags
)

const (
	ttrpcAddressEnv = "TTRPC_ADDRESS"
)

func parseFlags() {
	flag.BoolVar(&flags.debug, "debug", false, "enable debug output in logs")
	flag.BoolVar(&flags.version, "v", false, "show the shim version and exit")
	flag.StringVar(&flags.namespace, "namespace", "", "namespace that owns the shim")
	flag.StringVar(&flags.id, "id", "", "id of the task")
	flag.StringVar(&flags.socket, "socket", "", "socket path to serve")
	flag.StringVar(&flags.bundlePath, "bundle", "", "path to the bundle if not workdir")

	flag.StringVar(&flags.address, "address", "", "grpc address back to main containerd")
	flag.StringVar(&flags.containerdBinary, "publish-binary", "containerd", "path to publish binary (used for publishing events)")

	flag.Parse()
	flags.action = flag.Arg(0)
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
func Run(create CreateShim, opts ...BinaryOpts) {
	parseFlags()
	if flags.version {
		fmt.Printf("%s:\n", os.Args[0])
		fmt.Println("  Version: ", version.Version)
		fmt.Println("  Revision:", version.Revision)
		fmt.Println("  Go version:", version.GoVersion)
		fmt.Println("")
		return
	}
	setRuntime()

	var config Config
	for _, o := range opts {
		o(&config)
	}
	if err := run(create, config); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", shimID, err)
		os.Exit(1)
	}
}

func run(createShim CreateShim, config Config) error {
	signals, err := setupSignals(config)
	if err != nil {
		return err
	}
	if !config.NoSubreaper {
		if err := subreaper(); err != nil {
			return err
		}
	}
	if flags.bundlePath == "" {
		flags.bundlePath, err = os.Getwd()
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

	if flags.namespace == "" {
		return fmt.Errorf("shim namespace cannot be empty")
	}

	logger := logrus.WithFields(logrus.Fields{
		"id":        flags.id,
		"pid":       os.Getpid(),
		"path":      flags.bundlePath,
		"namespace": flags.namespace,
		"runtime":   shimID,
	})
	ctx := makeContext(logger)

	disabled, err := common.BundleStarted(ctx, flags.bundlePath)
	if err != nil {
		return err
	}
	if disabled {
		fmt.Println("bundle disabled, exit.")
		os.Exit(0)
		return nil
	}

	sigsCh := make(chan interface{}, 1)
	go handleSignals(sigsCh, logger, signals)

	switch flags.action {
	case "delete":
		return deleteCommand(ctx)
	case "start":
		if err := initializeSocket(ctx, InitSocketOpts{
			ID:      flags.id,
			Address: flags.address,
		}); err != nil {
			return err
		}
		return startShim(ctx, config, createShim, publisher, sigsCh)
	case "fork-start":
		return startNewProcessCommand(ctx, ttrpcAddress)
	default:
		return startShim(ctx, config, createShim, publisher, sigsCh)
	}
}

func makeContext(logger *logrus.Entry) context.Context {
	ctx := namespaces.WithNamespace(context.Background(), flags.namespace)
	ctx = context.WithValue(ctx, OptsKey{}, Opts{BundlePath: flags.bundlePath, Debug: flags.debug})
	return log.WithLogger(ctx, logger)
}

// clean up the whole bundle working directory by command, this will not require a running shim process
func deleteCommand(ctx context.Context) error {
	response, err := cleanup(ctx, flags.id)
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
}

func startNewProcessCommand(ctx context.Context, ttrpcAddress string) error {
	address, err := startNewProcess(ctx, StartProcessOpts{
		InitSocketOpts: InitSocketOpts{
			ID:      flags.id,
			Address: flags.address,
		},
		ContainerdBinary: flags.containerdBinary,
		TTRPCAddress:     ttrpcAddress,
	})
	if err != nil {
		return err
	}
	if _, err := os.Stdout.WriteString(address); err != nil {
		return err
	}
	return nil
}

// Start a command to run shim in new process
// Create Socket ant write socket address to bundle file
func startNewProcess(ctx context.Context, opts StartProcessOpts) (_ string, retErr error) {
	cmd, err := newCommand(ctx, opts.ID, opts.ContainerdBinary, opts.Address, opts.TTRPCAddress)
	if err != nil {
		return "", err
	}
	grouping := opts.ID
	spec, err := readSpec()
	if err != nil {
		return "", err
	}
	for _, group := range groupLabels {
		if groupID, ok := spec.Annotations[group]; ok {
			grouping = groupID
			break
		}
	}
	address, err := SocketAddress(ctx, opts.Address, grouping)
	if err != nil {
		return "", err
	}

	socket, err := NewSocket(address)
	if err != nil {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !SocketEaddrinuse(err) {
			return "", errors.Wrap(err, "create new shim socket")
		}
		if CanConnect(address) {
			if err := WriteAddress("address", address); err != nil {
				return "", errors.Wrap(err, "write existing socket for shim")
			}
			return address, nil
		}
		if err := RemoveSocket(address); err != nil {
			return "", errors.Wrap(err, "remove pre-existing socket")
		}
		if socket, err = NewSocket(address); err != nil {
			return "", errors.Wrap(err, "try create new shim socket 2x")
		}
	}
	defer func() {
		if retErr != nil {
			socket.Close()
			_ = RemoveSocket(address)
		}
	}()
	f, err := socket.File()
	if err != nil {
		return "", err
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, f)

	if err := cmd.Start(); err != nil {
		f.Close()
		return "", err
	}
	defer func() {
		if retErr != nil {
			cmd.Process.Kill()
		}
	}()
	// make sure to wait after start
	go cmd.Wait()
	if err := WriteAddress("address", address); err != nil {
		return "", err
	}
	if data, err := ioutil.ReadAll(os.Stdin); err == nil {
		if len(data) > 0 {
			var any ptypes.Any
			if err := proto.Unmarshal(data, &any); err != nil {
				return "", err
			}
			v, err := typeurl.UnmarshalAny(&any)
			if err != nil {
				return "", err
			}
			if opts, ok := v.(*options.Options); ok {
				if opts.ShimCgroup != "" {
					if cgroups.Mode() == cgroups.Unified {
						if err := cgroupsv2.VerifyGroupPath(opts.ShimCgroup); err != nil {
							return "", errors.Wrapf(err, "failed to verify cgroup path %q", opts.ShimCgroup)
						}
						cg, err := cgroupsv2.LoadManager("/sys/fs/cgroup", opts.ShimCgroup)
						if err != nil {
							return "", errors.Wrapf(err, "failed to load cgroup %s", opts.ShimCgroup)
						}
						if err := cg.AddProc(uint64(cmd.Process.Pid)); err != nil {
							return "", errors.Wrapf(err, "failed to join cgroup %s", opts.ShimCgroup)
						}
					} else {
						cg, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(opts.ShimCgroup))
						if err != nil {
							return "", errors.Wrapf(err, "failed to load cgroup %s", opts.ShimCgroup)
						}
						if err := cg.Add(cgroups.Process{
							Pid: cmd.Process.Pid,
						}); err != nil {
							return "", errors.Wrapf(err, "failed to join cgroup %s", opts.ShimCgroup)
						}
					}
				}
			}
		}
	}
	if err := AdjustOOMScore(cmd.Process.Pid); err != nil {
		return "", errors.Wrap(err, "failed to adjust OOM score for shim")
	}
	return address, nil
}

func newCommand(ctx context.Context, id, containerdBinary, containerdAddress, containerdTTRPCAddress string) (*exec.Cmd, error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-namespace", ns,
		"-id", id,
		"-address", containerdAddress,
	}
	cmd := exec.Command(self, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GOMAXPROCS=4")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	return cmd, nil
}

func initializeSocket(ctx context.Context, opts InitSocketOpts) error {
	// logrus.WithError(err).Warn("failed to remove runc container")
	var (
		retErr  error
		address string
	)

	grouping := opts.ID
	spec, err := readSpec()
	if err != nil {
		return err
	}
	for _, group := range groupLabels {
		if groupID, ok := spec.Annotations[group]; ok {
			grouping = groupID
			break
		}
	}
	address, err = SocketAddress(ctx, opts.Address, grouping)
	if err != nil {
		return err
	}

	socket, err := NewSocket(address)
	if err != nil {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !SocketEaddrinuse(err) {
			return errors.Wrapf(err, "create new shim socket")
		}
		if CanConnect(address) {
			if err := WriteAddress("address", address); err != nil {
				return errors.Wrapf(err, "write existing socket for shim")
			}
			// original code is return address, nil, nil
			// don't know when and how will get us here
			flags.socket = address
			return nil
		}
		if err := RemoveSocket(address); err != nil {
			return errors.Wrapf(err, "remove pre-existing socket")
		}
		if socket, err = NewSocket(address); err != nil {
			return errors.Wrapf(err, "try create new shim socket 2x")
		}
	}

	defer func() {
		if retErr != nil {
			socket.Close()
			_ = RemoveSocket(address)
		}
	}()

	if err := WriteAddress("address", address); err != nil {
		return err
	}

	flags.socket = address
	flags.socketListener = socket
	return nil
}

func startShim(ctx context.Context, config Config, createShim CreateShim, publisher *RemoteEventsPublisher, sigsCh chan interface{}) error {
	opts, err := common.LoadOpts(ctx, flags.bundlePath)
	if err != nil {
		return err
	}

	shimCh := make(chan interface{}, 1)
	var cancel func()
	ctx, cancel = context.WithCancel(ctx)
	go func() {
		select {
		case <-sigsCh:
		case <-shimCh:
		}
		cancel()
	}()

	if _, err := cleanup(ctx, flags.id); err != nil {
		logrus.WithError(err).Error("Cleanup error")
		return err
	}
	service, err := createShim(ctx, CreateShimOpts{
		ID:         flags.id,
		BundlePath: flags.bundlePath,
		CreateOpts: opts,
		Publisher:  publisher,
		Shutdown:   shimCh,
	})
	if err != nil {
		return err
	}
	if _, err = service.Start(ctx, &shimapi.StartRequest{
		ID: flags.id,
	}); err != nil {
		return err
	}
	return launch(ctx, ServeOpts{
		config:    config,
		service:   service,
		publisher: publisher,
	})
}

func cleanup(ctx context.Context, id string) (*shimapi.DeleteResponse, error) {
	log.G(ctx).WithField("id", id).Info("Begin Cleanup")
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(cwd), id)
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	runtime := "/usr/bin/runc"

	logrus.WithField("id", id).Info("ReadOptions")
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

	logrus.WithField("id", id).Info("Runc Delete")
	if err := r.Delete(ctx, id, &goRunc.DeleteOpts{
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

type ServeOpts struct {
	config    Config
	service   ShimService
	publisher *RemoteEventsPublisher
}

func launch(ctx context.Context, opts ServeOpts) error {
	logrus.Info("Launch")
	if !opts.config.NoSetupLogger {
		if err := setLogger(ctx, flags.id); err != nil {
			return err
		}
	}

	if err := serveShim(ctx, opts); err != nil {
		if err != context.Canceled {
			return err
		}
	}
	select {
	case <-opts.publisher.Done():
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("publisher not closed")
	}
}

// Serve the shim server
func serveShim(ctx context.Context, opts ServeOpts) error {
	logger := log.G(ctx)

	dump := make(chan os.Signal, 32)
	setupDumpStacks(dump)

	server, err := newServer()
	if err != nil {
		return errors.Wrap(err, "failed creating server")
	}

	logrus.Info("registering ttrpc server")
	shimapi.RegisterTaskService(server, TaskService{opts.service})

	if err := serveTTRPC(ctx, server, flags.socket); err != nil {
		logrus.Error("serve error")
		return err
	}
	go func() {
		for range dump {
			dumpStacks(logger)
		}
	}()
	<-ctx.Done()
	return ctx.Err()
}

func setLogger(ctx context.Context, id string) error {
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: log.RFC3339NanoFixed,
		FullTimestamp:   true,
	})
	if flags.debug {
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

// serve serves the ttrpc API over a unix socket at the provided path
// this function does not block
func serveTTRPC(ctx context.Context, server *ttrpc.Server, path string) error {
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
	if flags.socketListener == nil {
		l, err = serveListener(path)
		if err != nil {
			logrus.Errorf("serveListener failed, path = %s", path)
			return err
		}
	} else {
		l = flags.socketListener
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

type InitServiceOpts struct {
	service    ShimService
	id         string
	bundlePath string
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

func handleSignals(ch chan<- interface{}, logger *logrus.Entry, signals chan os.Signal) {
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
			close(ch)
		case unix.SIGINT:
			count++
			logger.Warn("sigal int")
			if count == 3 {
				logger.Warn("sigal int count > 3, terminating")
				close(ch)
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

func readSpec() (*spec, error) {
	f, err := os.Open("config.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var s spec
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}
