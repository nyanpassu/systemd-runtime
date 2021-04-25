package main

import (
	"context"
	"log"
	"os"

	"github.com/projecteru2/systemd-runtime/utils"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("Get WD error")
	}
	address, err := utils.ReceiveAddressOverFifo(context.Background(), wd)
	if err != nil {
		log.Fatalln("Get Address error")
	}
	log.Printf("address=%s\n", address)
}
