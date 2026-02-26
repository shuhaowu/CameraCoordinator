#!/bin/bash

set -xe

packaging/build.sh
cp packaging/build/*.deb build
