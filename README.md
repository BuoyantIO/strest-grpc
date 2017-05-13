# strest-grpc

Strest client and server implementations for gRPC.

## Running Locally

To run the client and server locally, first start the server.

```
$ go run server/main.go
starting gRPC server on :11111
```

Next run the client. By default, the client will send as many request as it can
on a single connection for 10 seconds, and exit with a performance report.

```
$ go run client/main.go --address localhost:11111
2017-05-12T16:17:40-07:00  98.2KB    354/0 10s L:   0 [ 89  97 ]  102 J:   0   0
{
  "good": 354,
  "bad": 0,
  "bytes": 100556,
  "latency": {
    "50": 10,
    "95": 89,
    "99": 97,
    "999": 102
  },
  "jitter": {
    "50": 0,
    "95": 0,
    "99": 0,
    "999": 0
  }
}
```

### Flags

Use the `-help` flag with either the client or server to see a list of flags.

## Building

To build the client and server binaries, run:

```
./bin/release.sh
```

That will create `strest-client-linux` and `strest-server-linux` binaries in the
root of the project.

To build a docker image, run:

```
$ docker build -t buoyantio/strest-grpc:latest .
```

Replace `latest` with whatever tag you are trying to build.

## Releasing

To release:

* Update and submit a PR with a changelog for the release in [CHANGES.md](CHANGES.md).
* Merge the changelog PR.
* Build the client and server binaries as described in the [Building](#building) section above.
* Use Github to [create a new release](https://github.com/BuoyantIO/strest-grpc/releases/new).
* Add the binaries that you built as attachments to the release.
* The docker image is built automatically once the release is created.
