#!/bin/bash

set -xe

mkdir -p build
go build -o build/camera-coordinator ./cmd/camera-coordinator
go build -o build/camera-coordinator-autolight ./autolight
sha256sum build/camera-coordinator* > build/checksums.txt
gzip build/camera-coordinator* # https://codeberg.org/woodpecker-plugins/plugin-s3/issues/328

# Make sure this still works, but we want to use the repo version to build.
go generate ./...
