package plugin

import (
	"os"

	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/platforms"
	"github.com/containerd/containerd/plugin"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/runtime"
)

// Config for the v2 runtime
type Config struct {
	// Supported platforms
	Platforms []string `toml:"platforms"`
}

func Register() {
	plugin.Register(&plugin.Registration{
		Type: plugin.RuntimePlugin,
		ID:   common.RuntimeName,
		Requires: []plugin.Type{
			plugin.MetadataPlugin,
		},
		Config: &Config{
			Platforms: defaultPlatforms(),
		},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			supportedPlatforms, err := parsePlatforms(ic.Config.(*Config).Platforms)
			if err != nil {
				return nil, err
			}

			ic.Meta.Platforms = supportedPlatforms
			if err := os.MkdirAll(ic.Root, 0711); err != nil {
				return nil, err
			}
			if err := os.MkdirAll(ic.State, 0711); err != nil {
				return nil, err
			}
			m, err := ic.Get(plugin.MetadataPlugin)
			if err != nil {
				return nil, err
			}
			cs := metadata.NewContainerStore(m.(*metadata.DB))

			return runtime.New(ic.Context, ic.Root, ic.State, ic.Address, ic.TTRPCAddress, ic.Events, cs)
		},
	})
}

func parsePlatforms(platformStr []string) ([]ocispec.Platform, error) {
	p := make([]ocispec.Platform, len(platformStr))
	for i, v := range platformStr {
		parsed, err := platforms.Parse(v)
		if err != nil {
			return nil, err
		}
		p[i] = parsed
	}
	return p, nil
}
