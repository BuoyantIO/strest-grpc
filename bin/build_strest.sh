#!/bin/sh

set -x

go build -o strest-server server/main.go
go build -o strest-client client/main.go
go build -o strest-max-rps max-rps/main.go

