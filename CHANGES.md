## In the next release...

## 0.0.8

* Introduce reference `ref-client` and `ref-server` for quickly diagnosing gRPC proxy behavior.
* Replace scratch with alpine for base Docker image which enables running a shell inside the image
* [client] Add the iteration number in client output
* [client] Fix client to throttle sends rather than receives
* [client] Add `--iterations` parameter to configure a budged for the experimet
* Add config and instruction for running in Kubernetes
* Migrate to Go modules.
* [client] Add `--requestLengthPercentiles` for controlling the size of the request payload

## 0.0.7

* [client] Fix `--latencyUnit` flag to correctly report metrics in the CLI and Prometheus.
* [client] For latency calculations, favor Go's native time types.

## 0.0.6

* Update to grpc-go 1.10.1.
* Docker images based on scratch images.
* [server] Add `--latencyPercentiles` to specify a latency distribution.
* [client] Fix default client address to match server.

## 0.0.5

* Add `--logLevel` flag, uses logrus.
* Update all Go dependencies, including grpc-go 1.2 -> 1.7.3.
* Update Docker build to use `golang:1.9.2-alpine` as a base.
* [client] Code cleanup.

## 0.0.4

* **Breaking Change** `client`, `server`, and `max-rps` now subcommands of `strest-grpc` executable.
* **Breaking Change** All CLI flags take two dashes now.
* Add support for Unix Domain sockets.
* [client] Support `--errorRate` for streaming requests.

## 0.0.3

* Add `strest-max-rps`, a tool for determining the max RPS of a backend.
* [client] Add `-totalTargetRps` to throttle requests to a fixed rate.
* [client] Rename `-concurrency` to `-connections`.
* [client] Add `-streams` to configure per-connection concurrency.
* [client] Add `-tlsTrustChainFile` to cause the client to establish and validate TLS connections.
* [server] Add `-tlsCertFile` and `-tlsPrivKeyFile`, which causes the server to accept TLS connections.
* [client] Do not add latency by default.
* [client] Do not add response payloads by default.

## 0.0.2

* Add a configurable `-clientTimeout` flag
* Add more info to the readme, including list of all available flags
* Fix prometheus stats reporting
* Fix unary request generator seg fault when request can't be sent
* Fix issue with partial intermediary reports not being printed
* Rename `-disableFinalReport` flag to `-noFinalReport`
* Rename `-onlyFinalReport` flag to `-noIntervalReport`

## 0.0.1

First release 🎈
