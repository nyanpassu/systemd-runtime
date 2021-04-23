package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/containerd/cgroups"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/containerd/typeurl"

	cgroupsv2 "github.com/containerd/cgroups/v2"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	ptypes "github.com/gogo/protobuf/types"

	"github.com/projecteru2/systemd-runtime/runshim"
	"github.com/projecteru2/systemd-runtime/utils"
)

var groupLabels = []string{
	"io.containerd.runc.v2.group",
	"io.kubernetes.cri.sandbox-id",
}

type spec struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

type shim struct {
	taskAPI.TaskService
}

// Create Socket ant write socket address to bundle file
func (s shim) StartShim(ctx context.Context, id, containerdBinary, containerdAddress, containerdTTRPCAddress string) (_ string, retErr error) {
	cmd, err := newCommand(ctx, id, containerdBinary, containerdAddress, containerdTTRPCAddress)
	if err != nil {
		return "", err
	}
	grouping := id
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
	address, err := runshim.SocketAddress(ctx, containerdAddress, grouping)
	if err != nil {
		return "", err
	}

	socket, err := runshim.NewSocket(address)
	if err != nil {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !runshim.SocketEaddrinuse(err) {
			return "", errors.Wrap(err, "create new shim socket")
		}
		if runshim.CanConnect(address) {
			if err := runshim.WriteAddress("address", address); err != nil {
				return "", errors.Wrap(err, "write existing socket for shim")
			}
			return address, nil
		}
		if err := runshim.RemoveSocket(address); err != nil {
			return "", errors.Wrap(err, "remove pre-existing socket")
		}
		if socket, err = runshim.NewSocket(address); err != nil {
			return "", errors.Wrap(err, "try create new shim socket 2x")
		}
	}
	defer func() {
		if retErr != nil {
			socket.Close()
			_ = runshim.RemoveSocket(address)
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
	if err := runshim.WriteAddress("address", address); err != nil {
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
	if err := runshim.AdjustOOMScore(cmd.Process.Pid); err != nil {
		return "", errors.Wrap(err, "failed to adjust OOM score for shim")
	}
	return address, nil
}

func (s shim) SystemdStartShim(ctx context.Context, id, containerdBinary, containerdAddress, containerdTTRPCAddress string) error {
	var retErr error

	grouping := id
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
	address, err := runshim.SocketAddress(ctx, containerdAddress, grouping)
	if err != nil {
		return err
	}

	socket, err := runshim.NewSocket(address)
	if err != nil {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !runshim.SocketEaddrinuse(err) {
			return errors.Wrapf(err, "create new shim socket")
		}
		if runshim.CanConnect(address) {
			if err := runshim.WriteAddress("address", address); err != nil {
				return errors.Wrapf(err, "write existing socket for shim")
			}
			return nil
		}
		if err := runshim.RemoveSocket(address); err != nil {
			return errors.Wrapf(err, "remove pre-existing socket")
		}
		if socket, err = runshim.NewSocket(address); err != nil {
			return errors.Wrapf(err, "try create new shim socket 2x")
		}
	}

	defer func() {
		if retErr != nil {
			socket.Close()
			_ = runshim.RemoveSocket(address)
		}
	}()

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err := runshim.WriteAddress("address", address); err != nil {
		return err
	}

	go func() {
		for {
			// send address over fifo
			_ = utils.SendAddressOverFifo(context.Background(), wd, address)
		}
	}()

	pid := os.Getpid()

	if data, err := ioutil.ReadAll(os.Stdin); err == nil {
		if len(data) > 0 {
			var any ptypes.Any
			if err := proto.Unmarshal(data, &any); err != nil {
				return err
			}
			v, err := typeurl.UnmarshalAny(&any)
			if err != nil {
				return err
			}
			if opts, ok := v.(*options.Options); ok {
				if opts.ShimCgroup != "" {
					if cgroups.Mode() == cgroups.Unified {
						if err := cgroupsv2.VerifyGroupPath(opts.ShimCgroup); err != nil {
							return errors.Wrapf(err, "failed to verify cgroup path %q", opts.ShimCgroup)
						}
						cg, err := cgroupsv2.LoadManager("/sys/fs/cgroup", opts.ShimCgroup)
						if err != nil {
							return errors.Wrapf(err, "failed to load cgroup %s", opts.ShimCgroup)
						}
						if err := cg.AddProc(uint64(pid)); err != nil {
							return errors.Wrapf(err, "failed to join cgroup %s", opts.ShimCgroup)
						}
					} else {
						cg, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(opts.ShimCgroup))
						if err != nil {
							return errors.Wrapf(err, "failed to load cgroup %s", opts.ShimCgroup)
						}
						if err := cg.Add(cgroups.Process{
							Pid: pid,
						}); err != nil {
							return errors.Wrapf(err, "failed to join cgroup %s", opts.ShimCgroup)
						}
					}
				}
			}
		}
	}
	return nil
}

// Cleanup is a binary call that cleans up any resources used by the shim when the service crashes
func (s shim) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	// cwd, err := os.Getwd()
	// if err != nil {
	// 	return nil, err
	// }
	// path := filepath.Join(filepath.Dir(cwd), s.id)
	// ns, err := namespaces.NamespaceRequired(ctx)
	// if err != nil {
	// 	return nil, err
	// }
	// runtime, err := container.ReadRuntime(path)
	// if err != nil {
	// 	return nil, err
	// }
	// opts, err := container.ReadOptions(path)
	// if err != nil {
	// 	return nil, err
	// }
	// root := process.RuncRoot
	// if opts != nil && opts.Root != "" {
	// 	root = opts.Root
	// }

	// r := runc.New(root, path, ns, runtime, "", false)
	// if err := r.Delete(ctx, s.id, &runc.DeleteOpts{
	// 	Force: true,
	// }); err != nil {
	// 	logrus.WithError(err).Warn("failed to remove runc container")
	// }
	// if err := mount.UnmountAll(filepath.Join(path, "rootfs"), 0); err != nil {
	// 	logrus.WithError(err).Warn("failed to cleanup rootfs mount")
	// }
	return &taskAPI.DeleteResponse{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
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
