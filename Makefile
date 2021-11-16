current_dir=$(shell pwd)

build:
	go build -o bin/eru-containerd cmd/eru-containerd/main.go
	go build -o bin/containerd-shim-eru-v2 cmd/eru-systemd-shim/main.go

install:
	cp bin/eru-containerd /usr/local/bin/
	cp bin/containerd-shim-eru-v2 /usr/local/bin/

grpc:
	protoc \
  --proto_path /usr/local/lib/protoc/include/ \
  --proto_path $(current_dir) \
 	--go_out=plugins=grpc:. \
	--go_opt=paths=source_relative \
	ctr/v1/ctr.proto

runc:
	go build -o bin/runc-test cmd/runc/main.go
