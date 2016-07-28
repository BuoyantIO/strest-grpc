#!/bin/bash

set -x

protoc -I protos protos/*.proto --go_out=plugins=grpc:protos
