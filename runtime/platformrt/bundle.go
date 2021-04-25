package platformrt

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"
	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/task"
)

func newBundle(ctx context.Context, root, state, id string, spec []byte) (task.Bundle, error) {
	if err := identifiers.Validate(id); err != nil {
		return nil, errors.Wrapf(err, "invalid task id %s", id)
	}

	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	work := filepath.Join(root, ns, id)
	b := &bundle{
		id:        id,
		path:      filepath.Join(state, ns, id),
		namespace: ns,
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
	err = ioutil.WriteFile(filepath.Join(b.path, task.ConfigFilename), spec, 0666)
	return b, err
}

func loadBundle(ctx context.Context, root, id string) (task.Bundle, error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	return &bundle{
		id:        id,
		path:      filepath.Join(root, ns, id),
		namespace: ns,
	}, nil
}

type bundle struct {
	id        string
	path      string
	namespace string
}

func (b *bundle) ID() string {
	return b.id
}

func (b *bundle) Delete() error {
	return task.DeletePath(b.path)
}

func (b *bundle) Path() string {
	return b.path
}

func (b *bundle) Namespace() string {
	return b.namespace
}

func (b *bundle) SaveOpts(ctx context.Context, opts runtime.CreateOpts) error {
	return task.SaveOpts(ctx, b, opts)
}

func (b *bundle) LoadOpts(ctx context.Context) (runtime.CreateOpts, error) {
	return task.LoadOpts(ctx, b.path)
}
