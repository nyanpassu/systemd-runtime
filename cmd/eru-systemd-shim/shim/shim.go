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
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	"github.com/containerd/containerd/sys/reaper"
	"github.com/containerd/containerd/version"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"golang.org/x/sys/unix"

	"github.com/containerd/typeurl"
	ptypes "github.com/gogo/protobuf/types"

	"github.com/containerd/cgroups"
	cgroupsv2 "github.com/containerd/cgroups/v2"

	"github.com/projecteru2/systemd-runtime/shim"
)

const (
	shimID          = "io.containerd.systemd.v1"
	ttrpcAddressEnv = "TTRPC_ADDRESS"
)

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

// Config of shim binary options provided by shim implementations
type Config struct {
	// NoSubreaper disables setting the shim as a child subreaper
	NoSubreaper bool
	// NoReaper disables the shim binary from reaping any child process implicitly
	NoReaper bool
	// NoSetupLogger disables automatic configuration of logrus to use the shim FIFO
	NoSetupLogger bool
}

// BinaryOpts allows the configuration of a shims binary setup
type BinaryOpts func(*Config)

// Run initializes and runs a shim server
func Run(opts ...BinaryOpts) {
	s := ShimApp{}
	if err := s.parseEnvAndFlags(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", shimID, err)
	}
	if s.printVersion() {
		return
	}
	setRuntime()

	for _, o := range opts {
		o(&s.config)
	}

	if err := s.run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", shimID, err)
		os.Exit(1)
	}
}

type ShimApp struct {
	debug   bool
	version bool
	// id the container id
	id                     string
	namespace              string
	socket                 string
	bundlePath             string
	containerdGRPCAddress  string
	containerdTTRPCAddress string
	containerdBinary       string
	action                 string
	startTimeout           int
	socketListener         net.Listener
	config                 Config
}

func (s *ShimApp) parseEnvAndFlags() (err error) {
	flag.BoolVar(&s.debug, "debug", false, "enable debug output in logs")
	flag.BoolVar(&s.version, "v", false, "show the shim version and exit")
	flag.StringVar(&s.namespace, "namespace", "", "namespace that owns the shim")
	flag.StringVar(&s.id, "id", "", "id of the task")
	flag.StringVar(&s.socket, "socket", "", "socket path to serve")
	flag.StringVar(&s.bundlePath, "bundle", "", "path to the bundle if not workdir")

	flag.StringVar(&s.containerdGRPCAddress, "address", "", "grpc address back to main containerd")
	flag.StringVar(&s.containerdBinary, "publish-binary", "containerd", "path to publish binary (used for publishing events)")
	flag.IntVar(&s.startTimeout, "start-timeout", 10, "timeout in seconds to start shim")

	flag.Parse()
	s.action = flag.Arg(0)

	if s.bundlePath == "" {
		if s.bundlePath, err = os.Getwd(); err != nil {
			return err
		}
	}
	if s.namespace == "" {
		return errors.New("shim namespace cannot be empty")
	}
	s.containerdTTRPCAddress = os.Getenv(ttrpcAddressEnv)
	if s.containerdTTRPCAddress == "" {
		return errors.New("ttrpc address cannot be empty")
	}
	return nil
}

func (s *ShimApp) printVersion() bool {
	if s.version || s.action == "version" {
		fmt.Printf("%s:\n", os.Args[0])
		fmt.Println("  Version: ", version.Version)
		fmt.Println("  Revision:", version.Revision)
		fmt.Println("  Go version:", version.GoVersion)
		fmt.Println("")
		return true
	}
	return false
}

func (s *ShimApp) run() error {
	signals, err := s.setupSignals()
	if err != nil {
		return err
	}
	if !s.config.NoSubreaper {
		if err := s.subreaper(); err != nil {
			return err
		}
	}

	publisher, err := shim.NewPublisher(s.containerdTTRPCAddress)
	if err != nil {
		return err
	}
	defer publisher.Close()

	logger := logrus.WithFields(logrus.Fields{
		"id":        s.id,
		"pid":       os.Getpid(),
		"path":      s.bundlePath,
		"namespace": s.namespace,
		"runtime":   shimID,
	})
	// for now we don't have a shim create timeout

	chShutdown := s.handleSignals(logger, signals)

	switch s.action {
	case "delete":
		return s.deleteCommand(s.makeContext(chShutdown, logger, 0), publisher)
	case "start":
		if err := s.initSocket(s.makeContext(chShutdown, logger, 0)); err != nil {
			return err
		}
		return s.startService(chShutdown, logger, publisher)
	case "fork-start":
		return s.startNewProcessCommand(s.makeContext(chShutdown, logger, 0))
	default:
		return s.startService(chShutdown, logger, publisher)
	}
}

func (s *ShimApp) makeContext(chShutdown <-chan struct{}, logger *logrus.Entry, timeout time.Duration) context.Context {
	ctx, cancel := context.WithCancel(namespaces.WithNamespace(log.WithLogger(context.Background(), logger), s.namespace))
	go func() {
		<-chShutdown
		cancel()
	}()
	return ctx
}

// clean up the whole bundle working directory by command, this will not require a running shim process
func (s *ShimApp) deleteCommand(ctx context.Context, publisher *shim.RemoteEventsPublisher) error {
	service, err := shim.NewShimService(ctx, shim.CreateShimOpts{
		ID:             s.id,
		BundlePath:     s.bundlePath,
		Publisher:      publisher,
		Debug:          s.debug,
		NoSetupLogger:  s.config.NoSetupLogger,
		Socket:         s.socket,
		SocketListener: s.socketListener,
	})
	if err != nil {
		return err
	}
	response, err := service.Cleanup(ctx)
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

func (s *ShimApp) startNewProcessCommand(ctx context.Context) error {
	address, err := s.startNewProcess(ctx)
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
func (s *ShimApp) startNewProcess(ctx context.Context) (_ string, retErr error) {
	cmd, err := s.newCommand(ctx)
	if err != nil {
		return "", err
	}
	grouping := s.id
	spec, err := s.readSpec()
	if err != nil {
		return "", err
	}
	for _, group := range groupLabels {
		if groupID, ok := spec.Annotations[group]; ok {
			grouping = groupID
			break
		}
	}
	address, err := socketAddress(ctx, s.containerdGRPCAddress, grouping)
	if err != nil {
		return "", err
	}

	socket, err := newSocket(address)
	if err != nil {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !SocketEaddrinuse(err) {
			return "", errors.Wrap(err, "create new shim socket")
		}
		if canConnect(address) {
			if err := writeAddress("address", address); err != nil {
				return "", errors.Wrap(err, "write existing socket for shim")
			}
			return address, nil
		}
		if err := RemoveSocket(address); err != nil {
			return "", errors.Wrap(err, "remove pre-existing socket")
		}
		if socket, err = newSocket(address); err != nil {
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
	if err := writeAddress("address", address); err != nil {
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

func (s *ShimApp) startService(chShutdown <-chan struct{}, logger *logrus.Entry, publisher *shim.RemoteEventsPublisher) error {
	ctx := s.makeContext(chShutdown, logger, 0)
	service, err := shim.NewShimService(ctx, shim.CreateShimOpts{
		ID:             s.id,
		BundlePath:     s.bundlePath,
		Publisher:      publisher,
		Debug:          s.debug,
		NoSetupLogger:  s.config.NoSetupLogger,
		Socket:         s.socket,
		SocketListener: s.socketListener,
	})
	if err != nil {
		return err
	}
	if err = service.InitContainer(ctx); err != nil {
		return err
	}
	if err = service.Serve(ctx); err != nil {
		return err
	}
	select {
	case <-publisher.Done():
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("publisher not closed")
	}
}

func (s *ShimApp) handleSignals(logger *logrus.Entry, signals chan os.Signal) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		logger.Info("starting signal loop")
		count := 0
		closed := false

		for sig := range signals {
			switch sig {
			case unix.SIGCHLD:
				if err := reaper.Reap(); err != nil {
					logger.WithError(err).Error("reap exit status")
				}
			case unix.SIGPIPE:
			case unix.SIGTERM:
				logger.Warn("sigal term")
				if !closed {
					close(ch)
					closed = true
				}
			case unix.SIGINT:
				count++
				logger.Warn("sigal int")
				if count == 3 {
					logger.Warn("sigal int count > 3, terminating")
					if !closed {
						close(ch)
						closed = true
					}
				}
			}
		}
	}()
	return ch
}

func (s *ShimApp) newCommand(ctx context.Context) (*exec.Cmd, error) {
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
		"-id", s.id,
		"-address", s.containerdGRPCAddress,
	}
	cmd := exec.Command(self, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GOMAXPROCS=4")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	return cmd, nil
}

func (s *ShimApp) initSocket(ctx context.Context) error {
	// logrus.WithError(err).Warn("failed to remove runc container")
	var (
		retErr  error
		address string
	)

	grouping := s.id
	spec, err := s.readSpec()
	if err != nil {
		return err
	}
	for _, group := range groupLabels {
		if groupID, ok := spec.Annotations[group]; ok {
			grouping = groupID
			break
		}
	}
	address, err = socketAddress(ctx, s.containerdGRPCAddress, grouping)
	if err != nil {
		return err
	}

	socket, err := newSocket(address)
	if err != nil {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !SocketEaddrinuse(err) {
			return errors.Wrapf(err, "create new shim socket")
		}
		if canConnect(address) {
			if err := writeAddress("address", address); err != nil {
				return errors.Wrapf(err, "write existing socket for shim")
			}
			// original code is return address, nil, nil
			// don't know when and how will get us here
			s.socket = address
			return nil
		}
		if err := RemoveSocket(address); err != nil {
			return errors.Wrapf(err, "remove pre-existing socket")
		}
		if socket, err = newSocket(address); err != nil {
			return errors.Wrapf(err, "try create new shim socket 2x")
		}
	}

	defer func() {
		if retErr != nil {
			socket.Close()
			_ = RemoveSocket(address)
		}
	}()

	if err := writeAddress("address", address); err != nil {
		return err
	}

	s.socket = address
	s.socketListener = socket
	return nil
}

// setupSignals creates a new signal handler for all signals and sets the shim as a
// sub-reaper so that the container processes are reparented
func (s *ShimApp) setupSignals() (chan os.Signal, error) {
	signals := make(chan os.Signal, 32)
	smp := []os.Signal{unix.SIGTERM, unix.SIGINT, unix.SIGPIPE}
	if !s.config.NoReaper {
		smp = append(smp, unix.SIGCHLD)
	}
	signal.Notify(signals, smp...)
	return signals, nil
}

func (s *ShimApp) subreaper() error {
	return reaper.SetSubreaper(1)
}

func (s *ShimApp) readSpec() (*spec, error) {
	f, err := os.Open("config.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var spec spec
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return nil, err
	}
	return &spec, nil
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
