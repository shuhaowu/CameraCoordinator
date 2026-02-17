package cameracoordinator

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -target amd64 -cc clang -cflags "-O2 -g -I$GOPATH/pkg/mod/github.com/cilium/ebpf@v0.20.0/examples/headers" cameraDetector bpf/camera_detector.bpf.c
