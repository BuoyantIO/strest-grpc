// Package client contains all strest-grpc client code. It supports both
// streaming and non-streaming configurations.

//
// control flow
//
// Run() ->
//   loop() (drives requests, aggregates responses, prints output, shuts down)
//   connect() (1x per connection) ->
//     sendStreamingRequests() (1x per stream. if streaming) ->
//       recvStream()
//       sendStream()
//     sendNonStreamingRequests() (1x per stream. if not streaming) ->
//       safeNonStreamingRequest()

//
// channel communication
//
// driver: throttles rate at which requests are sent
// shutdown: informs all request workers to shutdown
// responses: aggregates responses
// closeSend: informs streaming sender to close
//
// +--------+
// |        |   driver     +--------------+
// |        | -----------> | sendStream() | <------------+
// |        |              +------------- +   closeSend  |
// |        |                                            |
// |        |   shutdown   +-------------------------+   |
// |        | -----------> | sendStreamingRequests() | --+
// |        |              +-------------------------+
// |        |
// |        |   responses  +--------------+
// |        | <----------- | recvStream() |
// |        |              +--------------+
// |        |
// |        |                  streaming
// | loop() | ---------------------------------------------
// |        |                non-streaming
// |        |
// |        |   driver     +----------------------------+
// |        | -----------> |                            |
// |        |              | sendNonStreamingRequests() |
// |        |   shutdown   |                            |
// |        | -----------> |                            |
// |        |              +----------------------------+
// |        |
// |        |   responses  +---------------------------+
// |        | <----------- | safeNonStreamingRequest() |
// |        |              +---------------------------+
// +--------+

package client

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buoyantio/strest-grpc/distribution"
	"github.com/buoyantio/strest-grpc/percentiles"
	pb "github.com/buoyantio/strest-grpc/protos"
	"github.com/codahale/hdrhistogram"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// MeasuredResponse tracks the latency of a response and any
// accompanying error. timeBetweenFrames is how long we spend waiting
// for the next frame to arrive.
type MeasuredResponse struct {
	worker            workerID
	timeBetweenFrames time.Duration
	latency           time.Duration
	bytes             uint
	err               error
}

type workerID struct {
	connection, stream uint
}

func exUsage(msg string) {
	log.Fatalf("%s, see usage: %s client --help", msg, path.Base(os.Args[0]))
}

// Report provides top-level good/bad numbers and a latency breakdown.
type Report struct {
	Good    uint       `json:"good"`
	Bad     uint       `json:"bad"`
	Bytes   uint       `json:"bytes"`
	Latency *Quantiles `json:"latency"`
	Jitter  *Quantiles `json:"jitter"`
}

// Quantiles contains common latency quantiles (p50, p95, p999)
type Quantiles struct {
	Quantile50  int64 `json:"p50"`
	Quantile75  int64 `json:"p75"`
	Quantile90  int64 `json:"p90"`
	Quantile95  int64 `json:"p95"`
	Quantile99  int64 `json:"p99"`
	Quantile999 int64 `json:"p999"`
}

var (
	sizeSuffixes = []string{"B", "KB", "MB", "GB"}

	promRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "requests",
		Help: "Number of requests",
	})

	promSuccesses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "successes",
		Help: "Number of successful requests",
	})

	promLatencyMSHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "latency_ms",
		Help: "gRPC latency distributions in milliseconds.",
		// 50 exponential buckets ranging from 0.5 ms to 3 minutes
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(0.5, 1.3, 50),
	})
	promJitterMSHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "jitter_ms",
		Help: "gRPC jitter distributions in milliseconds.",
		// 50 exponential buckets ranging from 0.5 ms to 3 minutes
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(0.5, 1.3, 50),
	})

	promLatencyUSHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "latency_us",
		Help: "gRPC latency distributions in microseconds.",
		// 50 exponential buckets ranging from 1 us to 2.4 seconds
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(1, 1.35, 50),
	})
	promJitterUSHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "jitter_us",
		Help: "gRPC jitter distributions in microseconds.",
		// 50 exponential buckets ranging from 1 us to 2.4 seconds
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(1, 1.35, 50),
	})

	promLatencyNSHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "latency_ns",
		Help: "gRPC latency distributions in nanoseconds.",
		// 50 exponential buckets ranging from 1 ns to 0.4 seconds
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(1, 1.5, 50),
	})
	promJitterNSHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "jitter_ns",
		Help: "gRPC jitter distributions in nanoseconds.",
		// 50 exponential buckets ranging from 1 ns to 0.4 seconds
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(1, 1.5, 50),
	})
)

func registerMetrics() {
	prometheus.MustRegister(promRequests)
	prometheus.MustRegister(promSuccesses)
	prometheus.MustRegister(promLatencyMSHistogram)
	prometheus.MustRegister(promJitterMSHistogram)
	prometheus.MustRegister(promLatencyUSHistogram)
	prometheus.MustRegister(promJitterUSHistogram)
	prometheus.MustRegister(promLatencyNSHistogram)
	prometheus.MustRegister(promJitterNSHistogram)

}

func formatBytes(bytes uint) string {
	sz := float64(bytes)
	order := 0
	for order < len(sizeSuffixes) && sz >= 1024 {
		sz = sz / float64(1024)
		order++
	}
	return fmt.Sprintf("%0.1f%s", sz, sizeSuffixes[order])
}

// Periodically print stats about the request load.
func logIntervalReport(
	now time.Time,
	interval *time.Duration,
	iteration, good, bad, bytes, min, max uint,
	latencyHist *hdrhistogram.Histogram,
	jitterHist *hdrhistogram.Histogram) {
	if min == math.MaxInt64 {
		min = 0
	}
	fmt.Printf("%s %3d % 7s %6d/%1d %s L: %3d [%3d %3d ] %4d J: %3d %3d\n",
		now.Format(time.RFC3339),
		iteration,
		formatBytes(bytes),
		good,
		bad,
		interval,
		min,
		latencyHist.ValueAtQuantile(95),
		latencyHist.ValueAtQuantile(99),
		max,
		jitterHist.ValueAtQuantile(95),
		jitterHist.ValueAtQuantile(99),
	)
}

func logFinalReport(good, bad, bytes uint, latencies *hdrhistogram.Histogram, jitters *hdrhistogram.Histogram) {
	latency := Quantiles{
		Quantile50:  latencies.ValueAtQuantile(50),
		Quantile75:  latencies.ValueAtQuantile(75),
		Quantile90:  latencies.ValueAtQuantile(90),
		Quantile95:  latencies.ValueAtQuantile(95),
		Quantile99:  latencies.ValueAtQuantile(99),
		Quantile999: latencies.ValueAtQuantile(999),
	}

	jitter := Quantiles{
		Quantile50:  jitters.ValueAtQuantile(50),
		Quantile75:  jitters.ValueAtQuantile(75),
		Quantile90:  jitters.ValueAtQuantile(90),
		Quantile95:  jitters.ValueAtQuantile(95),
		Quantile99:  jitters.ValueAtQuantile(99),
		Quantile999: jitters.ValueAtQuantile(999),
	}
	report := Report{
		Good:    good,
		Bad:     bad,
		Bytes:   bytes,
		Latency: &latency,
		Jitter:  &jitter,
	}

	if data, err := json.MarshalIndent(report, "", "  "); err != nil {
		log.Fatal("Unable to generate report: ", err)
	} else {
		fmt.Println(string(data))
	}
}

func safeNonStreamingRequest(worker workerID,
	client pb.ResponderClient,
	clientTimeout time.Duration,
	lengthDistribution distribution.Distribution,
	latencyDistribution distribution.Distribution,
	errorRate float32, r *rand.Rand,
	responses chan<- *MeasuredResponse) {
	start := time.Now()

	var ctx context.Context
	if clientTimeout != time.Duration(0) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), clientTimeout)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	spec := pb.ResponseSpec{
		Length:    int32(lengthDistribution.Get(r.Int31() % 1000)),
		Latency:   latencyDistribution.Get(r.Int31() % 1000),
		ErrorRate: errorRate}

	log.Debugf("%v: req", worker)
	resp, err := client.Get(ctx, &spec)
	if err != nil {
		responses <- &MeasuredResponse{err: err}
		return
	}

	bytes := uint(len([]byte(resp.Body)))
	latency := time.Since(start)
	log.Debugf("%v: rsp %v", worker, latency)
	responses <- &MeasuredResponse{worker: worker, latency: latency, bytes: bytes}
}

func sendNonStreamingRequests(
	worker workerID,
	client pb.ResponderClient,
	shutdown <-chan struct{},
	clientTimeout time.Duration,
	lengthDistribution distribution.Distribution,
	latencyDistribution distribution.Distribution,
	errorRate float32,
	r *rand.Rand,
	driver <-chan struct{},
	responses chan<- *MeasuredResponse,
) {
	for {
		select {
		case <-shutdown:
			return
		case <-driver:
			safeNonStreamingRequest(worker, client, clientTimeout, lengthDistribution, latencyDistribution, errorRate, r, responses)
		}
	}
}

// parseStreamingRatio takes a string formatted as integer:integer,
// treats it as m:n and returns (m, n)
func parseStreamingRatio(streamingRatio string) (int64, int64) {
	possibleNumbers := strings.Split(streamingRatio, ":")

	if len(possibleNumbers) < 2 {
		log.Fatalf("streamingRatio '%s' didn't contain two numbers", streamingRatio)
	}

	var m, n int64
	var err error

	if m, err = strconv.ParseInt(possibleNumbers[0], 10, 64); err != nil {
		log.Fatalf("unable to parse left-hand side of streamingRatio '%s' due to error: '%v'", streamingRatio, err)
	}

	if n, err = strconv.ParseInt(possibleNumbers[1], 10, 64); err != nil {
		log.Fatalf("unable to parse right-hand side of streamingRatio '%s' due to error '%v'", streamingRatio, err)
	}

	return m, n
}

func recvStream(
	worker workerID,
	responses chan<- *MeasuredResponse,
	stream pb.Responder_StreamingGetClient,
) {
	for {
		start := time.Now()
		resp, err := stream.Recv()
		elapsed := time.Since(start)

		if err != nil {
			if err != io.EOF {
				s := status.Convert(err)
				code := s.Code()
				msg := s.Message()
				// ignore errors caused by process exiting
				if code != codes.Canceled &&
					(code != codes.Unavailable || msg != "transport is closing") {
					if code == codes.Unknown && strings.HasPrefix(msg, "strest-grpc stream error for ErrorRate: ") {
						// expected error based on ErrorRate
						responses <- &MeasuredResponse{err: err}
					} else {
						log.Errorf("stream.Recv() returned %s", err)
					}
				}
			}

			return
		}

		timeBetweenFrames := time.Duration(resp.CurrentFrameSent - resp.LastFrameSent)
		bytes := uint(len([]byte(resp.Body)))
		responses <- &MeasuredResponse{
			worker,
			timeBetweenFrames,
			elapsed,
			bytes,
			err,
		}
	}
}

func sendStream(
	driver <-chan struct{},
	lengthMap map[int32]int64,
	latencyMap map[int32]int64,
	errorRate float32,
	requestRatioM int64,
	requestRatioN int64,
	stream pb.Responder_StreamingGetClient,
	closeSend <-chan struct{},
) {
	defer stream.CloseSend()

	numRequests := int64(0)
	currentRequest := int64(0)

	for {
		select {
		case <-driver:
			if (currentRequest % requestRatioM) == 0 {
				numRequests = requestRatioN
			}

			err := stream.Send(&pb.StreamingResponseSpec{
				Count:              int32(numRequests),
				LatencyPercentiles: latencyMap,
				LengthPercentiles:  lengthMap,
				ErrorRate:          errorRate,
			})

			if err != nil {
				if err != io.EOF &&
					err.Error() != "rpc error: code = Internal desc = transport is closing" {
					log.Errorf("Failed to Send ResponseSpec: %v", err)
				}

				return
			}

			numRequests = 0
			currentRequest++
		case <-closeSend:
			return
		}
	}
}

func sendStreamingRequests(
	worker workerID,
	client pb.ResponderClient,
	shutdown <-chan struct{},
	lengthDistribution distribution.Distribution,
	latencyDistribution distribution.Distribution,
	errorRate float32,
	streamingRatio string,
	r *rand.Rand,
	driver <-chan struct{},
	responses chan<- *MeasuredResponse,
) {

	latencyMap := latencyDistribution.ToMap()
	lengthMap := lengthDistribution.ToMap()
	requestRatioM, requestRatioN := parseStreamingRatio(streamingRatio)

	for {
		stream, err := client.StreamingGet(context.Background())
		if err != nil {
			log.Fatalf("%v.StreamingGet(_) = _, %v", client, err)
		}

		// capacity == 2, to allow for either side to close, or both
		closing := make(chan struct{}, 2)
		closeSend := make(chan struct{})

		go func(stream pb.Responder_StreamingGetClient, closing chan<- struct{}) {
			recvStream(worker, responses, stream)
			closing <- struct{}{}
		}(stream, closing)

		go func(stream pb.Responder_StreamingGetClient, closing chan<- struct{}, closeSend <-chan struct{}) {
			sendStream(driver, lengthMap, latencyMap, errorRate, requestRatioM, requestRatioN, stream, closeSend)
			closing <- struct{}{}
		}(stream, closing, closeSend)

		exiting := false
		select {
		case <-closing:
		case <-shutdown:
			exiting = true
		}

		close(closeSend)

		if exiting {
			return
		}
	}
}

func connect(
	mainWait *sync.WaitGroup,
	cfg Config,
	driver <-chan struct{},
	responses chan<- *MeasuredResponse,
	shutdowns []chan struct{},
	connOpts []grpc.DialOption,
	c uint,
	latencyDistribution distribution.Distribution,
	lengthDistribution distribution.Distribution,
) {
	defer mainWait.Done()

	var connWait sync.WaitGroup
	connWait.Add(int(cfg.Streams))

	conn, err := grpc.Dial(cfg.Address, connOpts...)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	client := pb.NewResponderClient(conn)

	for s := uint(0); s < cfg.Streams; s++ {
		worker := workerID{connection: c, stream: s}
		shutdown := shutdowns[s]

		go func() {
			defer connWait.Done()

			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			if !cfg.Streaming {
				sendNonStreamingRequests(worker, client, shutdown, cfg.ClientTimeout,
					lengthDistribution, latencyDistribution,
					float32(cfg.ErrorRate), r, driver, responses,
				)
			} else {
				sendStreamingRequests(worker, client, shutdown,
					lengthDistribution, latencyDistribution,
					float32(cfg.ErrorRate), cfg.StreamingRatio, r, driver, responses,
				)
			}
		}()
	}

	connWait.Wait()
}

func loop(
	mainWait *sync.WaitGroup,
	cfg Config,
	driver chan<- struct{},
	responses <-chan *MeasuredResponse,
	shutdownChannels [][]chan struct{},
	latencyDur time.Duration,
) {
	defer mainWait.Done()

	var bytes, totalBytes, sendCount, recvCount, iteration, good, totalGood, bad, totalBad, max, min uint
	min = math.MaxInt64

	concurrency := cfg.Connections * cfg.Streams

	intervalReport := time.Tick(cfg.Interval)

	latencyDurNS := latencyDur.Nanoseconds()
	msInNS := time.Millisecond.Nanoseconds()
	usInNS := time.Microsecond.Nanoseconds()

	// dayInTimeUnits represents the number of time units (ms, us, or ns) in a 24-hour day.
	dayInTimeUnits := int64(24 * time.Hour / latencyDur)

	latencyHist := hdrhistogram.New(0, dayInTimeUnits, 3)
	globalLatencyHist := hdrhistogram.New(0, dayInTimeUnits, 3)
	jitterHist := hdrhistogram.New(0, dayInTimeUnits, 3)
	globalJitterHist := hdrhistogram.New(0, dayInTimeUnits, 3)

	cleanup := make(chan struct{}, 3)
	interrupt := make(chan os.Signal, 2)
	signal.Notify(interrupt, syscall.SIGINT)

	// Drive a single request.
	drive := func() {
		if cfg.TotalRequests > 0 && sendCount == cfg.TotalRequests {
			log.Debug("drive: done")
			return
		}

		if len(driver) == cap(driver) {
			return
		}

		sendCount++
		log.Debugf("drive: signal %d %d/%d", sendCount, len(driver), cap(driver))

		driver <- struct{}{}
	}

	var driveTick <-chan time.Time
	if cfg.TotalTargetRps > 0 {
		// If a target rps is specified, it will periodically notify the driving/reporting
		// goroutine that a full concurrency of work should be driven.
		driveTick = time.Tick(time.Second)
	} else {
		// If there is no target RPS, ensure there's enough capacity for all threads to
		// operate

		if cfg.Streaming {
			// If Streaming == true, endlessly drive() sends, as we won't drive() them
			// below based on responses. We can also assume that TotalRequests == 0.
			go func() {
				for {
					sendCount++
					driver <- struct{}{}
				}
			}()
		} else {
			for i := uint(0); i != concurrency; i++ {
				drive()
			}
		}
	}

	iteration = 0
	for {
		select {
		case <-interrupt:
			cleanup <- struct{}{}

		case <-cleanup:
			log.Debug("cleanup")
			for _, streams := range shutdownChannels {
				for _, shutdown := range streams {
					shutdown <- struct{}{}
				}
			}

			if !cfg.NoFinalReport {
				logFinalReport(totalGood, totalBad, totalBytes, globalLatencyHist, globalJitterHist)
			}
			return

		case <-driveTick:
			// When a target rps is set, it may fire several times a second so that a
			// full concurrency of work may be scheduled at each interval.

			n := cfg.TotalTargetRps

			if cfg.TotalRequests > 0 {
				r := cfg.TotalRequests - sendCount
				if r < n {
					n = r
				}
			}

			r := uint(cap(driver) - len(driver))
			if r < n {
				n = r
			}

			log.Debugf("drive: %d [%d:%d]", n, len(driver), cap(driver))
			for i := uint(0); i != n; i++ {
				drive()
			}

		case resp := <-responses:
			recvCount++
			promRequests.Inc()

			log.Debugf("recv %d from %v", recvCount, resp.worker)
			if resp.err != nil {
				bad++
				totalBad++
			} else {
				good++
				totalGood++
				promSuccesses.Inc()

				bytes += resp.bytes
				totalBytes += resp.bytes

				respLatencyNS := resp.latency.Nanoseconds()
				latency := respLatencyNS / latencyDurNS
				if latency < int64(min) {
					min = uint(latency)
				}
				if latency > int64(max) {
					max = uint(latency)
				}
				latencyHist.RecordValue(latency)
				globalLatencyHist.RecordValue(latency)
				promLatencyMSHistogram.Observe(float64(respLatencyNS / msInNS))
				promLatencyUSHistogram.Observe(float64(respLatencyNS / usInNS))
				promLatencyNSHistogram.Observe(float64(respLatencyNS))

				respJitterNS := resp.timeBetweenFrames.Nanoseconds()
				jitter := respJitterNS / latencyDurNS
				jitterHist.RecordValue(jitter)
				globalJitterHist.RecordValue(jitter)
				promJitterMSHistogram.Observe(float64(respJitterNS / msInNS))
				promJitterUSHistogram.Observe(float64(respJitterNS / usInNS))
				promJitterNSHistogram.Observe(float64(respJitterNS))
			}

			// TODO: Make this work for Streaming. Specifically, when ErrorRate,
			// TotalRequests, and StreamingRatio are all non-default, the value of
			// recvCount is non-deterministic, and cannot be relied upon to
			// terminate the process. This also means we cannot drive() sends based on
			// responses, as we'll starve sendStream().
			if !cfg.Streaming {
				if recvCount == cfg.TotalRequests {
					// N.B. recvCount > 0, so totalRequests >0
					cleanup <- struct{}{}
				} else if cfg.TotalTargetRps == 0 {
					// If there's no target rps, just restore capacity immediately.
					drive()
				}
			}

		case t := <-intervalReport:
			if cfg.NoIntervalReport {
				continue
			}
			logIntervalReport(t, &cfg.Interval, iteration, good, bad, bytes, min, max,
				latencyHist, jitterHist)
			iteration++
			if cfg.NumIterations > 0 && iteration >= cfg.NumIterations {
				cleanup <- struct{}{}
			}
			bytes, good, bad, max, min = 0, 0, 0, 0, math.MaxInt64
			latencyHist.Reset()
			jitterHist.Reset()
		}
	}
}

/// Configuration to run a client.
type Config struct {
	Address            string
	UseUnixAddr        bool
	ClientTimeout      time.Duration
	Connections        uint
	Streams            uint
	TotalRequests      uint
	TotalTargetRps     uint
	Interval           time.Duration
	NumIterations      uint
	LatencyPercentiles string
	LengthPercentiles  string
	ErrorRate          float64
	NoIntervalReport   bool
	NoFinalReport      bool
	Streaming          bool
	StreamingRatio     string
	MetricAddr         string
	LatencyUnit        string
	TlsTrustChainFile  string
}

// TODO: this would be much less ugly if the configuration was either stored in a struct, or used viper...
func (cfg Config) Run() {

	latencyDur := time.Millisecond
	if cfg.LatencyUnit == "ms" {
		latencyDur = time.Millisecond
	} else if cfg.LatencyUnit == "us" {
		latencyDur = time.Microsecond
	} else if cfg.LatencyUnit == "ns" {
		latencyDur = time.Nanosecond
	} else {
		log.Fatalf("latency unit should be [ms | us | ns].")
	}

	if cfg.NoIntervalReport && cfg.NoFinalReport {
		log.Fatalf("cannot use both -noIntervalReport and -noFinalReport.")
	}

	if cfg.Connections < 1 {
		exUsage("connections must be at least 1")
	}

	if cfg.Streams < 1 {
		exUsage("streams must be at least 1")
	}

	if cfg.Streaming && cfg.TotalRequests != 0 {
		log.Fatalf("cannot use both -streaming and -totalRequests.")
	}

	latencyPercentiles, err := percentiles.ParsePercentiles(cfg.LatencyPercentiles)
	if err != nil {
		log.Fatalf("latencyPercentiles was not valid: %v", err)
	}

	latencyDistribution, err := distribution.FromMap(latencyPercentiles)
	if err != nil {
		log.Fatalf("unable to create latency distribution: %v", err)
	}

	lengthPercentiles, err := percentiles.ParsePercentiles(cfg.LengthPercentiles)
	if err != nil {
		log.Fatalf("lengthPercentiles was not valid: %v", err)
	}

	lengthDistribution, err := distribution.FromMap(lengthPercentiles)
	if err != nil {
		log.Fatalf("unable to create length distribution: %v", err)
	}

	// By default, allow enough capacity for each worker.
	capacity := cfg.Connections * cfg.Streams

	if cfg.TotalTargetRps > 0 {
		// Allow several seconds worth of additional capacity in case things back up.
		capacity = cfg.TotalTargetRps * 10
	}

	// Items are sent to this channel to inform sending goroutines when to do work.
	driver := make(chan struct{}, capacity)

	responses := make(chan *MeasuredResponse, capacity)

	if cfg.MetricAddr != "" {
		registerMetrics()
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.ListenAndServe(cfg.MetricAddr, nil)
		}()
	}

	shutdownChannels := make([][]chan struct{}, cfg.Connections)
	for c := uint(0); c < cfg.Connections; c++ {
		shutdowns := make([]chan struct{}, cfg.Streams)
		for s := uint(0); s < cfg.Streams; s++ {
			shutdowns[s] = make(chan struct{})
		}

		shutdownChannels[c] = shutdowns
	}

	var connOpts []grpc.DialOption
	if cfg.TlsTrustChainFile != "" {
		creds, err := credentials.NewClientTLSFromFile(cfg.TlsTrustChainFile, "")
		if err != nil {
			log.Fatalf("invalid ca cert file: %v", err)
		}
		connOpts = append(connOpts, grpc.WithTransportCredentials(creds))
	} else {
		connOpts = append(connOpts, grpc.WithInsecure())
	}

	af := "tcp"
	if cfg.UseUnixAddr {
		af = "unix"

		// Override the authority so it doesn't include something illegal like a path.
		connOpts = append(connOpts, grpc.WithAuthority("strest.local"))
	}

	dial := func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(af, addr, timeout)
	}
	connOpts = append(connOpts, grpc.WithDialer(dial))

	var mainWait sync.WaitGroup

	for c := uint(0); c < cfg.Connections; c++ {
		c := c
		shutdowns := shutdownChannels[c]

		// initiate requests and keep them alive
		mainWait.Add(1)
		go connect(
			&mainWait,
			cfg,
			driver,
			responses,
			shutdowns,
			connOpts,
			c,
			latencyDistribution,
			lengthDistribution,
		)
	}

	// main event loop, parses responses
	mainWait.Add(1)
	go loop(
		&mainWait,
		cfg,
		driver,
		responses,
		shutdownChannels,
		latencyDur,
	)

	mainWait.Wait()
	os.Exit(0)
}
