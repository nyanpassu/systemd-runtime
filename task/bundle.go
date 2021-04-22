package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/runtime"
	"github.com/pkg/errors"
)

const ConfigFilename = "config.json"

const optsFileName = "opts"

type Bundle interface {
	ID() string
	Delete() error
	Path() string
	Namespace() string
	SaveOpts(context.Context, runtime.CreateOpts) error
	LoadOpts(context.Context) (runtime.CreateOpts, error)
}

// Delete a bundle atomically
func DeletePath(bundlePath string) error {
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

func SaveOpts(ctx context.Context, b Bundle, opts runtime.CreateOpts) error {
	content, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	if err = ioutil.WriteFile(filepath.Join(b.Path(), optsFileName), content, 0666); err != nil {
		return err
	}
	return nil
}

func LoadOpts(ctx context.Context, b Bundle) (runtime.CreateOpts, error) {
	var (
		opts runtime.CreateOpts
	)
	f, err := os.Open(filepath.Join(b.Path(), optsFileName))
	if err != nil {
		return runtime.CreateOpts{}, err
	}
	content, err := ioutil.ReadAll(f)
	if err != nil {
		return runtime.CreateOpts{}, err
	}
	if err = json.Unmarshal(content, &opts); err != nil {
		return runtime.CreateOpts{}, err
	}
	return opts, nil
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
