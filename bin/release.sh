#!/bin/sh

set -ex

VERSION="${1:-latest}"

export GOARCH=amd64

for GOOS in darwin linux ; do
    dir="release/$GOOS"
    mkdir -p "$dir"

    export GOOS
    go build -o "$dir/strest-grpc" main.go

    tar -cvzf release/strest-grpc-$VERSION-$GOOS.tar.gz -C $dir .
done
