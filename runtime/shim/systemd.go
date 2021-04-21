package shim

import (
	"github.com/projecteru2/systemd-runtime/systemd"

	systemdRuntime "github.com/projecteru2/systemd-runtime/runtime"
)

func detail(b systemdRuntime.Bundle) systemd.Detail {
	return systemd.Detail{
		Unit: systemd.UnitSector{
			Description: "",
		},
		Service: systemd.ServiceSector{
			Type:      "",
			ExecStart: []string{systemdRuntime.ShimBinaryName, ""},
		},
		Install: systemd.InstallSector{
			WantedBy: "multi-user.target",
		},
	}
}
