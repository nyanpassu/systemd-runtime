package containerd

import (
	"github.com/projecteru2/systemd-runtime/systemd"

	task "github.com/projecteru2/systemd-runtime/task"
)

func detail(b task.Bundle) systemd.Detail {
	return systemd.Detail{
		Unit: systemd.UnitSector{
			Description: "EruSystemdUnit-" + b.ID(),
		},
		Service: systemd.ServiceSector{
			Type:      "fork",
			ExecStart: []string{task.ShimBinaryName},
		},
		Install: systemd.InstallSector{
			WantedBy: "multi-user.target",
		},
	}
}
