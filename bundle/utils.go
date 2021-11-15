package bundle

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"

	specsGo "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/projecteru2/systemd-runtime/common"
)

const ConfigFilename = "config.json"

// NewBundle .
func NewBundle(
	ctx context.Context,
	root, state, id, ns, containerdAddress, containerdTTRPCAddress string,
	exchange *exchange.Exchange,
	opts runtime.CreateOpts,
) (_ common.Bundle, err error) {
	if err := identifiers.Validate(id); err != nil {
		return nil, errors.Wrapf(err, "invalid task id %s", id)
	}
	bundlePath := bundlePath(state, id, ns)
	work := filepath.Join(root, ns, id)
	b := &Bundle{
		id:                     id,
		path:                   bundlePath,
		namespace:              ns,
		containerdAddress:      containerdAddress,
		containerdTTRPCAddress: containerdTTRPCAddress,
		exchange:               exchange,
	}
	unit, err := b.CreateSystemdUnit(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = unit.DisableIfPresent(ctx)
			_ = unit.DeleteIfPresent(ctx)
		}
	}()
	if err := unit.Enable(ctx); err != nil {
		return nil, err
	}
	var paths []string
	defer func() {
		if err != nil {
			for _, d := range paths {
				os.RemoveAll(d)
			}
		}
	}()
	// create state directory for the bundle
	if err := os.MkdirAll(filepath.Dir(b.path), 0711); err != nil {
		return nil, err
	}
	if err := os.Mkdir(b.path, 0711); err != nil {
		return nil, err
	}
	paths = append(paths, b.path)
	// create working directory for the bundle
	if err := os.MkdirAll(filepath.Dir(work), 0711); err != nil {
		return nil, err
	}
	rootfs := filepath.Join(b.path, "rootfs")
	if err := os.MkdirAll(rootfs, 0711); err != nil {
		return nil, err
	}
	paths = append(paths, rootfs)
	if err := os.Mkdir(work, 0711); err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
		os.RemoveAll(work)
		if err := os.Mkdir(work, 0711); err != nil {
			return nil, err
		}
	}
	paths = append(paths, work)
	// symlink workdir
	if err := os.Symlink(work, filepath.Join(b.path, "work")); err != nil {
		return nil, err
	}
	// we will remote hooks and copy overlay fs to new folder
	spec, err := processSpec(b.path, opts.Spec.Value)
	if err != nil {
		return nil, err
	}
	err = ioutil.WriteFile(filepath.Join(b.path, ConfigFilename), spec, 0666) //nolint:gosec
	if err != nil {
		return nil, err
	}

	err = common.SaveOpts(ctx, bundlePath, opts)
	if err != nil {
		return nil, err
	}
	statusManager, err := common.NewStatusManager(bundlePath, log.G(ctx))
	if err != nil {
		return nil, err
	}
	b.statusManager = statusManager
	return b, err
}

// LoadBundle .
func LoadBundle(ctx context.Context, state, id, namespace string, events *exchange.Exchange) (common.Bundle, error) {
	bundlePath := bundlePath(state, id, namespace)
	statusManager, err := common.NewStatusManager(bundlePath, log.G(ctx))
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{
		id:            id,
		path:          bundlePath,
		namespace:     namespace,
		statusManager: statusManager,
		exchange:      events,
	}
	// fast path
	bf, err := ioutil.ReadDir(bundle.Path())
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fast path read bundle path for %s", bundle.path)
		return nil, err
	}
	if len(bf) == 0 {
		return nil, errors.New("bundle is empty")
	}
	return bundle, nil
}

// DeleteBundle a bundle atomically
func DeleteBundle(state, id, namespace string) error {
	return common.DeleteBundlePath(filepath.Join(state, namespace, id))
}

func bundlePath(state, id, ns string) string {
	return filepath.Join(state, ns, id)
}

func processSpec(bundle string, content []byte) ([]byte, error) {
	spec := &specsGo.Spec{}
	if err := json.Unmarshal(content, spec); err != nil {
		return nil, err
	}
	// can't support apparmor yet
	// systemd unit will start early then apparmor ready
	spec.Process.ApparmorProfile = ""
	if err := processRootfs(bundle, spec); err != nil {
		return nil, err
	}
	// docker network require a running dockerd, we don't support yet
	if err := processNetworkNamespace(spec); err != nil {
		return nil, err
	}

	content, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func processNetworkNamespace(spec *specsGo.Spec) error {
	if spec.Hooks != nil && spec.Hooks.Prestart != nil {
		return errors.New("only support host network")
	}

	if spec.Linux == nil {
		return nil
	}

	if spec.Linux.Sysctl != nil {
		delete(spec.Linux.Sysctl, "net.ipv4.ip_unprivileged_port_start")
	}

	var namespaces []specsGo.LinuxNamespace
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type != "network" {
			namespaces = append(namespaces, ns)
		}
	}
	spec.Linux.Namespaces = namespaces
	return nil
}

func processRootfs(bundle string, spec *specsGo.Spec) error {
	rootfs := spec.Root.Path
	if rootfs == "" || strings.HasPrefix(rootfs, bundle) {
		return nil
	}

	newRoot := filepath.Join(bundle, "merged")
	if err := copyRoot(spec.Root.Path, newRoot); err != nil {
		return err
	}

	spec.Root.Path = newRoot
	return nil
}

func copyRoot(src string, dst string) error {
	cmd := exec.Command("cp", "-r", "-p", src, dst)
	return cmd.Run()
}
