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

package common

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	"github.com/containerd/containerd/namespaces"
	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/utils"
)

var runtimePaths sync.Map

const (
	shimBinaryFormat = "containerd-shim-%s-%s"
	socketPathLimit  = 106
)

// Command returns the shim command with the provided args and configuration
func Command(ctx context.Context, runtime, containerdAddress, containerdTTRPCAddress, path string, opts *types.Any, cmdArgs ...string) (*exec.Cmd, error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-namespace", ns,
		"-address", containerdAddress,
		"-publish-binary", self,
	}
	args = append(args, cmdArgs...)
	name := BinaryName(runtime)
	if name == "" {
		return nil, fmt.Errorf("invalid runtime name %s, correct runtime name should format like io.containerd.runc.v1", runtime)
	}

	var cmdPath string
	cmdPathI, cmdPathFound := runtimePaths.Load(name)
	if cmdPathFound {
		cmdPath = cmdPathI.(string)
	} else {
		var lerr error
		if cmdPath, lerr = exec.LookPath(name); lerr != nil {
			if eerr, ok := lerr.(*exec.Error); ok {
				if eerr.Err == exec.ErrNotFound {
					// LookPath only finds current directory matches based on
					// the callers current directory but the caller is not
					// likely in the same directory as the containerd
					// executables. Instead match the calling binaries path
					// (containerd) and see if they are side by side. If so
					// execute the shim found there.
					testPath := filepath.Join(filepath.Dir(self), name)
					if _, serr := os.Stat(testPath); serr == nil {
						cmdPath = testPath
					}
					if cmdPath == "" {
						return nil, errors.Wrapf(os.ErrNotExist, "runtime %q binary not installed %q", runtime, name)
					}
				}
			}
		}
		cmdPath, err = filepath.Abs(cmdPath)
		if err != nil {
			return nil, err
		}
		if cmdPathI, cmdPathFound = runtimePaths.LoadOrStore(name, cmdPath); cmdPathFound {
			// We didn't store cmdPath we loaded an already cached value. Use it.
			cmdPath = cmdPathI.(string)
		}
	}

	cmd := exec.Command(cmdPath, args...)
	cmd.Dir = path
	cmd.Env = append(
		os.Environ(),
		"GOMAXPROCS=2",
		fmt.Sprintf("%s=%s", TTRPCAddressEnv, containerdTTRPCAddress),
	)
	cmd.SysProcAttr = getSysProcAttr()
	if opts != nil {
		d, err := proto.Marshal(opts)
		if err != nil {
			return nil, err
		}
		cmd.Stdin = bytes.NewReader(d)
	}
	return cmd, nil
}

// Command returns the shim command with the provided args and configuration
func SystemdCommand(namespace, runtime, containerdAddress, containerdTTRPCAddress, path string, opts *types.Any, cmdArgs ...string) (*utils.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-namespace", namespace,
		"-address", containerdAddress,
		"-publish-binary", self,
	}
	args = append(args, cmdArgs...)
	name := BinaryName(runtime)
	if name == "" {
		return nil, fmt.Errorf("invalid runtime name %s, correct runtime name should format like io.containerd.runc.v1", runtime)
	}

	var cmdPath string
	cmdPathI, cmdPathFound := runtimePaths.Load(name)
	if cmdPathFound {
		cmdPath = cmdPathI.(string)
	} else {
		var lerr error
		if cmdPath, lerr = exec.LookPath(name); lerr != nil {
			if eerr, ok := lerr.(*exec.Error); ok {
				if eerr.Err == exec.ErrNotFound {
					// LookPath only finds current directory matches based on
					// the callers current directory but the caller is not
					// likely in the same directory as the containerd
					// executables. Instead match the calling binaries path
					// (containerd) and see if they are side by side. If so
					// execute the shim found there.
					testPath := filepath.Join(filepath.Dir(self), name)
					if _, serr := os.Stat(testPath); serr == nil {
						cmdPath = testPath
					}
					if cmdPath == "" {
						return nil, errors.Wrapf(os.ErrNotExist, "runtime %q binary not installed %q", runtime, name)
					}
				}
			}
		}
		cmdPath, err = filepath.Abs(cmdPath)
		if err != nil {
			return nil, err
		}
		if cmdPathI, cmdPathFound = runtimePaths.LoadOrStore(name, cmdPath); cmdPathFound {
			// We didn't store cmdPath we loaded an already cached value. Use it.
			cmdPath = cmdPathI.(string)
		}
	}

	cmd := utils.Cmd{
		CmdPath:     cmdPath,
		Args:        args,
		WorkingPath: path,
		Env: append(
			os.Environ(),
			"GOMAXPROCS=2",
			fmt.Sprintf("%s=%s", TTRPCAddressEnv, containerdTTRPCAddress),
		),
	}

	return &cmd, nil
}

// BinaryName returns the shim binary name from the runtime name,
// empty string returns means runtime name is invalid
func BinaryName(runtime string) string {
	// runtime name should format like $prefix.name.version
	parts := strings.Split(runtime, ".")
	if len(parts) < 2 {
		return ""
	}

	return fmt.Sprintf(shimBinaryFormat, parts[len(parts)-2], parts[len(parts)-1])
}

func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}
