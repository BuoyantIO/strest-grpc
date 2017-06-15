#!/bin/sh

set -x

GOOS=linux GOARCH=amd64 go build -o strest-server-linux server/main.go
GOOS=linux GOARCH=amd64 go build -o strest-client-linux client/main.go
GOOS=linux GOARCH=amd64 go build -o strest-max-rps-linux max-rps/main.go
