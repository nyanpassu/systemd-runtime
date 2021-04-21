package systemd

import (
	"fmt"
	"os"
	"strings"

	"github.com/projecteru2/systemd-runtime/utils"
)

var BasePath = "/lib/systemd/system"

type UnitSector struct {
	Description string
}

type ServiceSector struct {
	Type             string
	WorkingDirectory string
	ExecStart        []string
	Environment      []string
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
	envs := []string{}
	for _, env := range detail.Service.Environment {
		envs = append(envs, fmt.Sprintf("Environment=\"%s\"", env))
	}
	return fmt.Sprintf(
		"[Service]\nType=%s\nWorkingDirectory=%s\n%s\nExecStart=%s",
		detail.Service.Type,
		detail.Service.WorkingDirectory,
		strings.Join(envs, "\n"),
		strings.Join(detail.Service.ExecStart, " "),
	)
}

func (detail Detail) install() string {
	return fmt.Sprintf("[Install]\nWantedBy=%s", detail.Install.WantedBy)
}

func GenerateSystemdUnitFile(name string, detail Detail) error {
	f, err := os.OpenFile(getAbsPath(name), os.O_RDWR|os.O_CREATE|os.O_SYNC, 0644)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(detail.String()); err != nil {
		return err
	}
	return f.Close()
}

func DeleteSystemdUnitFileIfPresent(name string) error {
	exists, err := UnitFileExists(name)
	if err != nil {
		return err
	}
	if exists {
		return os.Remove(getAbsPath(name))
	}
	return nil
}

func UnitFileExists(name string) (bool, error) {
	return utils.FileExists(getAbsPath(name))
}

func getAbsPath(name string) string {
	return fmt.Sprintf("%s/%s.service", BasePath, name)
}
