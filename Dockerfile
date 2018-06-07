FROM golang:1.10.2 as build
WORKDIR /go/src/strest-grpc
ADD . /go/src/github.com/buoyantio/strest-grpc
RUN CGO_ENABLED=0 go build -installsuffix cgo -o /go/bin/strest-grpc /go/src/github.com/buoyantio/strest-grpc/main.go

FROM scratch
COPY --from=build /go/bin /go/bin
ENTRYPOINT ["/go/bin/strest-grpc"]
