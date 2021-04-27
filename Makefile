build:
	GO111MODULE=off go build -o bin/eru-containerd cmd/eru-containerd/main.go
	GO111MODULE=off go build -o bin/containerd-shim-eru-v2 cmd/eru-systemd-shim/main.go
	GO111MODULE=off go build -o bin/get-address cmd/get-address/main.go
	GO111MODULE=off go build -o bin/connect-shim cmd/connect-shim/main.go
	GO111MODULE=off go build -o bin/ctr2 cmd/ctr/main.go

install:
	cp bin/eru-containerd /usr/local/bin/
	cp bin/containerd-shim-eru-v2 /usr/local/bin/
	cp bin/get-address /usr/local/bin/
	cp bin/connect-shim /usr/local/bin/
	cp bin/ctr2 /usr/local/bin/