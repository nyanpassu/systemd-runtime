package main

import (
	"encoding/json"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cli "github.com/urfave/cli/v2"

	eventstypes "github.com/containerd/containerd/api/events"
)

const (
	eventTypeFlag = "event-type"
	eventDataFlag = "event-data"
)

func publish(ctx *cli.Context) error {
	p := socketPath(ctx)
	t := ctx.String(eventTypeFlag)
	d := ctx.String(eventDataFlag)

	evt, err := parseEvent(t, d)
	if err != nil {
		return err
	}
	evtAny, err := ptypes.MarshalAny(evt)
	if err != nil {
		return err
	}
	c, close, err := client(p)
	if err != nil {
		return err
	}
	defer func() {
		if err := close(); err != nil {
			log.WithError(err).Error("close grpc conn error")
		}
	}()
	_, err = c.Publish(ctx.Context, evtAny)
	return err
}

func parseEvent(evtType string, data string) (proto.Message, error) {
	var evt proto.Message
	switch evtType {
	case "TaskStart":
		evt = &eventstypes.TaskStart{}
	case "TaskCreate":
		evt = &eventstypes.TaskCreate{}
	case "TaskDelete":
		evt = &eventstypes.TaskDelete{}
	case "TaskExit":
		evt = &eventstypes.TaskExit{}
	case "TaskExecAdded":
		evt = &eventstypes.TaskExecAdded{}
	case "TaskExecStarted":
		evt = &eventstypes.TaskExecStarted{}
	default:
		return nil, errors.New("unsupport event type")
	}
	if err := json.Unmarshal([]byte(data), evt); err != nil {
		return nil, err
	}
	return evt, nil
}

func event() *cli.Command {
	return &cli.Command{
		Name:   "publish",
		Usage:  "publish event",
		Action: publish,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     eventTypeFlag,
				Required: true,
			},
			&cli.StringFlag{
				Name:     eventDataFlag,
				Required: true,
			},
		},
	}
}
