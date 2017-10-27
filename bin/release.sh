#!/bin/sh

set -ex

export GOARCH=amd64

for GOOS in darwin linux ; do
    dir="release/$GOOS"
    mkdir -p "$dir"

    export GOOS
    go build -o "$dir/strest-grpc" main.go
done
