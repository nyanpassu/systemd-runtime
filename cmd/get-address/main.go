package main

import (
	"context"
	"log"
	"os"

	"github.com/projecteru2/systemd-runtime/common"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("Get WD error")
	}
	address, err := common.ReceiveAddressOverFifo(context.Background(), wd)
	if err != nil {
		log.Fatalln("Get Address error")
	}
	log.Printf("address=%s\n", address)
}
