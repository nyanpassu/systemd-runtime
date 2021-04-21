package common

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/runtime"
)

const optsFileName = "opts"

func SaveOpts(ctx context.Context, path string, opts runtime.CreateOpts) error {
	content, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	if err = ioutil.WriteFile(filepath.Join(path, optsFileName), content, 0666); err != nil {
		return err
	}
	return nil
}

func LoadOpts(ctx context.Context, bundlePath string) (runtime.CreateOpts, error) {
	var (
		opts runtime.CreateOpts
	)
	f, err := os.Open(filepath.Join(bundlePath, optsFileName))
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
