#!/bin/bash

COVERAGE_FILE=build/coverage.out

set -xe

mkdir -p build
timeout -v 30s go test -v -coverprofile=${COVERAGE_FILE} ./...
go tool cover -func=${COVERAGE_FILE}
go tool cover -html=${COVERAGE_FILE} -o ${COVERAGE_FILE}.html
