FROM golang:1.8.0-alpine

WORKDIR /go/src/strest-grpc

ADD . /go/src/github.com/buoyantio/strest-grpc

RUN go build -o /go/bin/strest-server /go/src/github.com/buoyantio/strest-grpc/server/main.go
RUN go build -o /go/bin/strest-client /go/src/github.com/buoyantio/strest-grpc/client/main.go

ENTRYPOINT ["/go/bin/strest-server"]
