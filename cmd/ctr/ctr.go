package main

import (
	"os"

	log "github.com/sirupsen/logrus"
	cli "github.com/urfave/cli/v2"
)

const (
	address = "address"
)

func main() {
	if err := (&cli.App{
		Name:    "systemd runtime shim ctr",
		Usage:   "systemd runtime shim ctr",
		Version: "0.0.0",
		Commands: []*cli.Command{
			event(),
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     address,
				Aliases:  []string{"a"},
				Usage:    "socket path",
				Required: true,
			},
		},
	}).Run(os.Args); err != nil {
		log.Fatalln(err)
	}
}

func socketPath(ctx *cli.Context) string {
	return ctx.String(address)
}
