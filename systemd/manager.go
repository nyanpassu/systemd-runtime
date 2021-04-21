package systemd

import (
	"context"

	"github.com/containerd/containerd/log"
)

func Create(ctx context.Context, name string, detail Detail) (*Unit, error) {
	log.G(ctx).WithField("name", name).Debug("create systemd unit file")

	exists, err := UnitFileExists(name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrUnitExists
	}
	if err = GenerateSystemdUnitFile(name, detail); err != nil {
		return nil, err
	}
	return &Unit{
		Name: name,
	}, nil
}

func GetUnit(ctx context.Context, name string) (*Unit, error) {
	exists, err := UnitFileExists(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrUnitNotExists
	}
	return &Unit{
		Name: name,
	}, nil
}

func DeleteUnitIfPresent(ctx context.Context, name string) error {
	return DeleteSystemdUnitFileIfPresent(name)
}

func doIfPresent(ctx context.Context, name string, f func() error) error {
	exists, err := UnitFileExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return f()
}
