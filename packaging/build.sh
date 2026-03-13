#!/usr/bin/env bash
set -euo pipefail

# Run this script from the root of the repo.

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

NOW=$(date -u +%Y%m%d%H%M%S)

# build_pkg <package-dir> <go-source-dir> <binary-name>
build_pkg() {
  local pkg_dir="$DIR/$1"
  local src_dir="$2"
  local bin_name="${3:-$1}"
  local build_dir="$pkg_dir/deb-build"

  rm -rf "$build_dir"
  mkdir -p "$build_dir/usr/local/bin"

  echo "building ${bin_name} for package $1..."
  go build -o "$build_dir/usr/local/bin/$bin_name" "$src_dir"

  echo "copying packaging files for $1..."
  if [ -d "$pkg_dir/files" ]; then
    cp -a "$pkg_dir/files/." "$build_dir/"
  fi

  if [ -d "$pkg_dir/debian" ]; then
    mkdir -p "$build_dir/DEBIAN"
    cp -a "$pkg_dir/debian/." "$build_dir/DEBIAN/"
  fi

  echo "building .deb for $1..."

  # try to read Version and Architecture from control file
  version=""
  arch=""
  if [ -f "$pkg_dir/debian/control" ]; then
    version=$(awk '/^Version:/{print $2; exit}' "$pkg_dir/debian/control" || true)
    arch=$(awk '/^Architecture:/{print $2; exit}' "$pkg_dir/debian/control" || true)
  fi
  # git sha of the repository containing the packaging dir
  sha=$(git -C "$DIR" rev-parse --short HEAD 2>/dev/null)

  OUT_DEB="$DIR/build/${1}_${version}+${NOW}+${sha}_${arch}.deb"
  dpkg-deb --root-owner-group --build "$build_dir" "$OUT_DEB"

  echo "built package: $OUT_DEB"
}

rm -rf "$DIR/build"
mkdir -p "$DIR/build"
# Build both packages using the common function. Add or remove calls as needed.
build_pkg "camera-coordinator" "./cmd/camera-coordinator" "camera-coordinator"
# build_pkg "camera-coordinator-autolight" "./autolight" "camera-coordinator-autolight"
