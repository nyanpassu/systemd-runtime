package main

import (
	"context"
	"io"
	"os"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/utils"
	log "github.com/sirupsen/logrus"
)

func main() {
	read1()
}

func read1() {
	bundlePath := os.Args[1]

	statusFile, err := common.OpenShimStatusFile(bundlePath)
	if err != nil {
		log.WithError(err).Fatalln("open shim status file error")
	}

	content, err := io.ReadAll(statusFile)
	if err != nil {
		log.WithError(err).Fatalln("read shim status file error")
	}

	log.Info(content)
}

func read() {

	bundlePath := os.Args[1]

	statusFile, err := common.OpenShimStatusFile(bundlePath)
	if err != nil {
		log.WithError(err).Fatalln("open shim status file error")
	}
	shimLockFile, err := common.OpenShimLockFile(bundlePath)
	if err != nil {
		log.WithError(err).Fatalln("open shim lock file error")
	}
	ctx := context.Background()
	if err := utils.FileLock(ctx, statusFile); err != nil {
		log.WithError(err).Fatalln("lock shim status file error")
	}
	defer func() {
		if err := utils.FileUnlock(statusFile); err != nil {
			log.WithError(err).Error("unlock status file error")
		}
	}()
	status := common.ShimStatus{}
	if _, err := utils.FileReadJSON(statusFile, &status); err != nil {
		log.WithError(err).Fatalln("read shim status file error")
	}
	canLock, err := utils.FileCanLock(shimLockFile)
	if err != nil {
		log.WithError(err).Fatalln("test shim lock file error")
	}
	log.Info("can lock %b", canLock)
}
