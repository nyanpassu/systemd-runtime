package bundle

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/runtime"

	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/common"
)

const ConfigFilename = "config.json"

// NewBundle .
func NewBundle(
	ctx context.Context,
	root, state, id, ns, containerdAddress, containerdTTRPCAddress string,
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
	// write the spec to the bundle
	err = ioutil.WriteFile(filepath.Join(b.path, ConfigFilename), opts.Spec.Value, 0666)

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

// LoadAndCheckBundle .
func LoadBundle(ctx context.Context, state, id, namespace string) (common.Bundle, error) {
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
	}
	// fast path
	bf, err := ioutil.ReadDir(bundle.Path())
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fast path read bundle path for %s", bundle.Path)
		return nil, err
	}
	if len(bf) == 0 {
		return nil, errors.New("bundle is empty")
	}
	return bundle, nil
}

// Delete a bundle atomically
func DeleteBundle(state, id, namespace string) error {
	return deletePath(filepath.Join(state, namespace, id))
}

// atomicDelete renames the path to a hidden file before removal
func atomicDelete(path string) error {
	// create a hidden dir for an atomic removal
	atomicPath := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s", filepath.Base(path)))
	if err := os.Rename(path, atomicPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.RemoveAll(atomicPath)
}

func bundlePath(state, id, ns string) string {
	return filepath.Join(state, ns, id)
}

func deletePath(bundlePath string) error {
	work, werr := os.Readlink(filepath.Join(bundlePath, "work"))
	rootfs := filepath.Join(bundlePath, "rootfs")
	if err := mount.UnmountAll(rootfs, 0); err != nil {
		return errors.Wrapf(err, "unmount rootfs %s", rootfs)
	}
	if err := os.Remove(rootfs); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "failed to remove bundle rootfs")
	}
	err := atomicDelete(bundlePath)
	if err == nil {
		if werr == nil {
			return atomicDelete(work)
		}
		return nil
	}
	// error removing the bundle path; still attempt removing work dir
	var err2 error
	if werr == nil {
		err2 = atomicDelete(work)
		if err2 == nil {
			return err
		}
	}
	return errors.Wrapf(err, "failed to remove both bundle and workdir locations: %v", err2)
}
