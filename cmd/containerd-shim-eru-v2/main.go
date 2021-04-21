package main

import (
	"github.com/projecteru2/systemd-runtime/cmd/containerd-shim-eru-v2/shim"

	eruShim "github.com/projecteru2/systemd-runtime/runtime/v2/shim"
)

func main() {
	// init and execute the shim
	shim.Run("io.containerd.systemd.v1", eruShim.New)
}
