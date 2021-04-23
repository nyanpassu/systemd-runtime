package platformrt

import (
	"github.com/projecteru2/systemd-runtime/systemd"

	"github.com/projecteru2/systemd-runtime/runtime"
	"github.com/projecteru2/systemd-runtime/task"
)

func detail(b task.Bundle) systemd.Detail {
	return systemd.Detail{
		Unit: systemd.UnitSector{
			Description: "EruSystemdUnit-" + b.ID(),
		},
		Service: systemd.ServiceSector{
			Type:      "fork",
			ExecStart: []string{runtime.ShimBinaryName},
		},
		Install: systemd.InstallSector{
			WantedBy: "multi-user.target",
		},
	}
}

func name(id string) string {
	return "eru-systemd-" + id
}
