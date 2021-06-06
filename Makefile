build:
	GO111MODULE=off go build -o bin/eru-containerd cmd/eru-containerd/main.go
	GO111MODULE=off go build -o bin/containerd-shim-eru-v2 cmd/containerd-shim-eru-v2/main.go
	GO111MODULE=off go build -o bin/eru-systemd-runc cmd/eru-systemd-runc/main.go