[![CircleCI](https://circleci.com/gh/BuoyantIO/strest-grpc.svg?style=shield)](https://circleci.com/gh/BuoyantIO/strest-grpc)

# strest-grpc

Strest client and server implementations for gRPC.

## Running Locally

To run the client and server locally, first start the server.

```bash
$ go run main.go server
starting gRPC server on :11111
```

Next run the client. By default, the client will send as many request as it can
on a single connection for 10 seconds, and exit with a performance report.

```bash
$ go run main.go client --address localhost:11111
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

### Usage

Use the `--help` flag for usage information.

#### Commands

```bash
$ go run main.go --help
A load tester for stress testing grpc intermediaries.

Find more information at https://github.com/buoyantio/strest-grpc.

Usage:
  strest-grpc [command]

Available Commands:
  client      run the strest-grpc client
  help        Help about any command
  max-rps     compute max RPS
  server      run the strest-grpc server

Flags:
  -h, --help              help for strest-grpc
  -l, --logLevel string   log level, must be one of: panic, fatal, error, warn, info, debug (default "info")

Use "strest-grpc [command] --help" for more information about a command.
```

#### Client

```bash
$ go run main.go client --help
run the strest-grpc client

Usage:
  strest-grpc client [flags]

Flags:
      --address string              address of strest-grpc service or intermediary (default "localhost:11111")
      --clientTimeout duration      timeout for unary client requests. Default: no timeout
      --connections uint            number of concurrent connections (default 1)
      --errorRate float             the chance to return an error
  -h, --help                        help for client
      --interval duration           reporting interval (default 10s)
      --latencyPercentiles string   response latency percentile distribution. (e.g. 50=10,100=100) (default "100=0")
      --latencyUnit string          latency units [ms|us|ns] (default "ms")
      --lengthPercentiles string    response body length percentile distribution. (e.g. 50=100,100=1000) (default "100=0")
      --metricAddr string           address to serve metrics on
      --noFinalReport               do not print a final JSON output report
      --noIntervalReport            only print the final report, nothing intermediate
      --streaming                   use the streaming features of strest server
      --streamingRatio string       the ratio of streaming requests/responses (default "1:1")
      --streams uint                number of concurrent streams per connection (default 1)
      --tlsTrustChainFile string    the path to the certificate used to validate the remote's signature
      --totalRequests uint          total number of requests to send. default: infinite
      --totalTargetRps uint         target requests per second
  -u, --unix                        use Unix Domain Sockets instead of TCP

Global Flags:
  -l, --logLevel string   log level, must be one of: panic, fatal, error, warn, info, debug (default "info")
```

#### Server

```bash
$ go run main.go server --help
run the strest-grpc server

Usage:
  strest-grpc server [flags]

Flags:
      --address string          address to serve on (default ":11111")
  -h, --help                    help for server
      --metricAddr string       address to serve metrics on
      --tlsCertFile string      the path to the trust certificate
      --tlsPrivKeyFile string   the path to the server's private key
  -u, --unix                    use Unix Domain Sockets instead of TCP

Global Flags:
  -l, --logLevel string   log level, must be one of: panic, fatal, error, warn, info, debug (default "info")
```

#### Max-RPS

```bash
$ go run main.go max-rps --help
compute max RPS

Usage:
  strest-grpc max-rps [flags]

Flags:
      --address string             hostname:port of strest-grpc service or intermediary (default "localhost:11111")
      --concurrencyLevels string   levels of concurrency to test with (default "1,5,10,20,30")
  -h, --help                       help for max-rps
      --timePerLevel duration      how much time to spend testing each concurrency level (default 1s)

Global Flags:
  -l, --logLevel string   log level, must be one of: panic, fatal, error, warn, info, debug (default "info")
```

## Building

To build the strest-grpc binaries and archives, run:

```bash
./bin/release.sh [VERSION TAG]
```

That will create `strest-grpc` binaries and archives in `./release`.

To build a docker image, run:

```bash
$ docker build -t buoyantio/strest-grpc:latest .
```

Replace `latest` with whatever tag you are trying to build.

### `strest-grpc max-rps`

`strest-grpc max-rps` will calculate the maximum RPS that a strest-server or intermediary
can sustain. The `maxConcurrency` score lets you know what at which point adding more
clients no longer improves overall throughput. This calculation is based on the
Universal Scalability Law and can change dramatically based on the environment and
resources available.

## Releasing

To release:

* Update and submit a PR with a changelog for the release in [CHANGES.md](CHANGES.md).
* Merge the changelog PR.
* Build the strest-grpc archives as described in the [Building](#building) section above.
* Use Github to [create a new release](https://github.com/BuoyantIO/strest-grpc/releases/new).
* Add the archives that you built as attachments to the release.
* The docker image is built automatically once the release is created.
