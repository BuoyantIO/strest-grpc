FROM golang:1.11.1-stretch as build
ADD . /go/src/github.com/buoyantio/strest-grpc
RUN CGO_ENABLED=0 go build -installsuffix cgo -o /go/bin/strest-grpc /go/src/github.com/buoyantio/strest-grpc/main.go

FROM alpine:3.8
RUN apk --update upgrade && \
    apk add ca-certificates curl nghttp2 && \
    update-ca-certificates && \
    rm -rf /var/cache/apk/*
COPY --from=build /go/bin /go/bin
ENTRYPOINT ["/go/bin/strest-grpc"]
