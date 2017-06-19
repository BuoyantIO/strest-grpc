[![CircleCI](https://circleci.com/gh/BuoyantIO/strest-grpc.svg?style=shield)](https://circleci.com/gh/BuoyantIO/strest-grpc)

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

| Flag                  | Default   | Description |
|-----------------------|-----------|-------------|
| `-address`            | `localhost:11111` | hostname:port of strest-grpc service or intermediary. |
| `-clientTimeout`      | `<none>`  | Timeout for unary client request. No timeout is applied if unset. |
| `-concurrency`        | `1`       | Number of goroutines to run, each at the specified QPS level. Measure total QPS as `qps * concurrency`. |
| `-interval`           | `10s`     | How often to report stats to stdout. |
| `-latencyPercentiles` | `50=10,100=100` | response latency percentile distribution in milliseconds. |
| `-lengthPercentiles`  | `50=100,100=1000` | response body length percentile. |
| `-metric-addr`        | `<none>`  | Address to use when serving the Prometheus `/metrics` endpoint. No metrics are served if unset. Format is `host:port` or `:port`. |
| `-noFinalReport`      | `<unset>` | If set, don't print the json latency report at the end. |
| `-noIntervalReport`   | `<unset>` | If set, only print the final report, nothing intermediate. |
| `-streaming`          | `<unset>` | response is a gRPC stream from the strest server. |
| `-streamingRatio`     | `1:1`     | the ratio of streaming requests/responses if streaming is enabled. |
| `-totalRequests`      | `<none>`  | Exit after sending this many requests. |
| `-help`               | `<unset>` | If set, print all available flags and exit. |

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
