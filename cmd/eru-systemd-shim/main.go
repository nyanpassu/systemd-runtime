package main

import (
	"github.com/projecteru2/systemd-runtime/runtime/shim"

	eruShim "github.com/projecteru2/systemd-runtime/runtime/shim/shim"
)

func main() {
	// init and execute the shim
	shim.Run("io.containerd.systemd.v1", eruShim.New)
}
