#!/bin/bash

set -xe

# Doesn't work right now due to: https://github.com/woodpecker-ci/plugin-git/issues/322
packaging/build.sh
cp packaging/build/*.deb build
