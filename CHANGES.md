## Upcoming: 0.1.0

* **Breaking Change** `client`, `server`, and `max-rps` now subcommands of `strest-grpc` executable.
* **Breaking Change** All CLI flags take two dashes now.
* Add support for Unix Domain sockets.

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
