build:
	go build -o bin/eru-containerd cmd/eru-containerd/main.go
	go build -o bin/containerd-shim-eru-v2 cmd/eru-systemd-shim/main.go
	go build -o bin/get-address cmd/get-address/main.go
	go build -o bin/connect-shim cmd/connect-shim/main.go

install:
	cp bin/eru-containerd /usr/local/bin/
	cp bin/containerd-shim-eru-v2 /usr/local/bin/

runc:
	go build -o bin/runc-test cmd/runc/main.go

