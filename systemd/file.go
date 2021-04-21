package systemd

import (
	"fmt"
	"os"
	"strings"

	"github.com/projecteru2/systemd-runtime/utils"
)

type UnitSector struct {
	Description string
}

type ServiceSector struct {
	Type      string
	ExecStart []string
}

type InstallSector struct {
	WantedBy string
}

type Detail struct {
	Unit    UnitSector
	Service ServiceSector
	Install InstallSector
}

func (detail Detail) String() string {
	return fmt.Sprintf("%s\n%s\n%s\n", detail.unit(), detail.service(), detail.install())
}

func (detail Detail) unit() string {
	return fmt.Sprintf("[Unit]\nDescription=%s", detail.Unit.Description)
}

func (detail Detail) service() string {
	return fmt.Sprintf("[Service]\nType=%s\nExecStart=%s", detail.Service.Type, strings.Join(detail.Service.ExecStart, " "))
}

func (detail Detail) install() string {
	return fmt.Sprintf("[Install]\nWantedBy=%s", detail.Install.WantedBy)
}

// FileManager .
type FileManager struct {
	Base string
}

func (m *FileManager) GenerateSystemdUnitFile(name string, detail Detail) error {
	f, err := os.OpenFile(m.getAbsPath(name), os.O_RDWR|os.O_CREATE|os.O_SYNC, 0644)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(detail.String()); err != nil {
		return err
	}
	return f.Close()
}

func (m *FileManager) RemoveSystemdUnitFile(name string) error {
	exists, err := m.UnitFileExists(name)
	if err != nil {
		return err
	}
	if exists {
		return os.Remove(m.getAbsPath(name))
	}
	return nil
}

func (m *FileManager) UnitFileExists(name string) (bool, error) {
	return utils.FileExists(m.getAbsPath(name))
}

func (m *FileManager) getAbsPath(name string) string {
	return fmt.Sprintf("%s/%s.service", m.Base, name)
}
