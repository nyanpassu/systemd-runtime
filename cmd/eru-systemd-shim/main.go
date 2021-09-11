package main

import (
	"github.com/projecteru2/systemd-runtime/shim"
	"github.com/projecteru2/systemd-runtime/shim/service"
)

func main() {
	service.Run(func(conf *shim.Config) {
		conf.NoSetupLogger = true
	})
}
