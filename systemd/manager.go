package systemd

import (
	"context"
)

func NewUnitManager(base string) *UnitManager {
	return &UnitManager{
		fileManager: FileManager{
			Base: base,
		},
	}
}

type UnitManager struct {
	fileManager FileManager
}

func (m *UnitManager) Create(ctx context.Context, name string, detail Detail) (*Unit, error) {
	exists, err := m.fileManager.UnitFileExists(name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrUnitExists
	}
	if err = m.fileManager.GenerateSystemdUnitFile(name, detail); err != nil {
		return nil, err
	}
	return &Unit{
		Name:    name,
		manager: m,
	}, nil
}

func (m *UnitManager) Get(ctx context.Context, name string) (*Unit, error) {
	exists, err := m.fileManager.UnitFileExists(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrUnitNotExists
	}
	return &Unit{
		Name:    name,
		manager: m,
	}, nil
}

func (m *UnitManager) DoIfPresent(ctx context.Context, name string, f func() error) error {
	exists, err := m.fileManager.UnitFileExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return f()
}

func (m *UnitManager) DeleteIfPresent(ctx context.Context, name string) error {
	return m.fileManager.DeleteSystemdUnitFileIfPresent(name)
}
