package containerd

import (
	"github.com/projecteru2/systemd-runtime/systemd"

	task "github.com/projecteru2/systemd-runtime/task"
)

func detail(b task.Bundle) systemd.Detail {
	return systemd.Detail{
		Unit: systemd.UnitSector{
			Description: "",
		},
		Service: systemd.ServiceSector{
			Type:      "",
			ExecStart: []string{task.ShimBinaryName, ""},
		},
		Install: systemd.InstallSector{
			WantedBy: "multi-user.target",
		},
	}
}
