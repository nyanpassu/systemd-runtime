package main

import (
	"github.com/projecteru2/systemd-runtime/shim"
)

func main() {
	shim.Run(func(conf *shim.Config) {
		conf.NoSetupLogger = true
	})
}
