package client

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
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

	"github.com/buoyantio/strest-grpc/client/distribution"
	"github.com/buoyantio/strest-grpc/client/percentiles"
	pb "github.com/buoyantio/strest-grpc/protos"
	"github.com/codahale/hdrhistogram"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/transport"
)

// MeasuredResponse tracks the latency of a response and any
// accompaning error. timeBetweenFrames is how long we spend waiting
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
	fmt.Fprintf(os.Stderr, "%s\n", msg)
	fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
	flag.PrintDefaults()
	os.Exit(64)
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
	Quantile50  int64 `json:"50"`
	Quantile95  int64 `json:"95"`
	Quantile99  int64 `json:"99"`
	Quantile999 int64 `json:"999"`
}

// DayInMillis represents the number of milliseconds in a 24-hour day.
const DayInMillis int64 = 24 * 60 * 60 * 1000000

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

	promLatencyHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "latency_ms",
		Help: "gRPC latency distributions in milliseconds.",
		// 50 exponential buckets ranging from 0.5 ms to 3 minutes
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(0.5, 1.3, 50),
	})
	promJitterHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "jitter_ms",
		Help: "gRPC jitter distributions in milliseconds.",
		// 50 exponential buckets ranging from 0.5 ms to 3 minutes
		// TODO: make this tunable
		Buckets: prometheus.ExponentialBuckets(0.5, 1.3, 50),
	})
)

func registerMetrics() {
	prometheus.MustRegister(promRequests)
	prometheus.MustRegister(promSuccesses)
	prometheus.MustRegister(promLatencyHistogram)
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
	good, bad, bytes, min, max uint,
	latencyHist *hdrhistogram.Histogram,
	jitterHist *hdrhistogram.Histogram) {
	if min == math.MaxInt64 {
		min = 0
	}
	fmt.Printf("%s % 7s %6d/%1d %s L: %3d [%3d %3d ] %4d J: %3d %3d\n",
		now.Format(time.RFC3339),
		formatBytes(bytes),
		good,
		bad,
		interval,
		min/1000000,
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
		Quantile95:  latencies.ValueAtQuantile(95),
		Quantile99:  latencies.ValueAtQuantile(99),
		Quantile999: latencies.ValueAtQuantile(999),
	}

	jitter := Quantiles{
		Quantile50:  jitters.ValueAtQuantile(50),
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

	//fmt.Printf("%v: req\n", worker)
	resp, err := client.Get(ctx, &spec)
	if err != nil {
		responses <- &MeasuredResponse{err: err}
		return
	}

	bytes := uint(len([]byte(resp.Body)))
	latency := time.Since(start)
	//fmt.Printf("%v: rsp %v\n", worker, latency)
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
		log.Fatalf("streamingRatio '%s' didn't contain two numbers\n", streamingRatio)
	}

	var m, n int64
	var err error

	if m, err = strconv.ParseInt(possibleNumbers[0], 10, 64); err != nil {
		log.Fatalf("unable to parse left-hand side of streamingRatio '%s' due to error: '%v'\n", streamingRatio, err)
	}

	if n, err = strconv.ParseInt(possibleNumbers[1], 10, 64); err != nil {
		log.Fatalf("unable to parse right-hand side of streamingRatio '%s' due to error '%v'\n", streamingRatio, err)
	}

	return m, n
}

func recvStream(
	worker workerID,
	responses chan<- *MeasuredResponse,
	stream pb.Responder_StreamingGetClient,
	receiving <-chan struct{},
	recvErr chan<- struct{},
) {
	for {
		select {

		case _, more := <-receiving:
			if more {
				// we should never get here
				log.Fatalf("Invalid state: message sent on receiving channel")
			}

			return

		default:
			start := time.Now()
			resp, err := stream.Recv()
			elapsed := time.Since(start)

			if err != nil {
				if err == io.EOF {
					// read done.
					log.Println("stream.Recv() returned io.EOF")
				} else if err.Error() == status.New(codes.Internal, transport.ErrConnClosing.Desc).Err().Error() {
					// THIS IS WHERE STUFF ACTUALLY RETURNS ON SUCCESS
					// check for "rpc error: code = Internal desc = transport is closing"
					log.Println("stream.Recv() returned transport is closing")
				} else {
					// THIS IS WHERE STUFF ACTUALLY RETURNS ON FAIL
					log.Println("stream.Recv() ERR FAIL: " + err.Error())
					responses <- &MeasuredResponse{err: err}
				}

				// stream.CloseSend()
				recvErr <- struct{}{}
				return
			}

			log.Println("RECV")
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
}

func sendStream(
	driver <-chan struct{},
	lengthMap map[int32]int64,
	latencyMap map[int32]int64,
	errorRate float32,
	requestRatioM int64,
	requestRatioN int64,
	sending <-chan struct{},
	sendErr chan<- struct{},
	worker workerID,
	client pb.ResponderClient,
	responses chan<- *MeasuredResponse,
) {
	receiving := make(chan struct{})
	defer close(receiving)
	recvErr := make(chan struct{})

	stream, err := client.StreamingGet(context.Background())
	if err != nil {
		log.Fatalf("%v.StreamingGet(_) = _, %v", client, err)
	}
	defer stream.CloseSend()

	go recvStream(worker, responses, stream, receiving, recvErr)

	numRequests := int64(0)
	currentRequest := int64(0)

	for {
		select {
		case <-driver:
			log.Println("sendStream::<-driver")
			if (currentRequest % requestRatioM) == 0 {
				numRequests = requestRatioN
			}

			log.Println("sendStream::<-driver send PRE")
			err := stream.Send(&pb.StreamingResponseSpec{
				Count:              int32(numRequests),
				LatencyPercentiles: latencyMap,
				LengthPercentiles:  lengthMap,
				ErrorRate:          errorRate,
			})
			log.Printf("sendStream::<-driver send POST returned %+v\n", err)

			if err == io.EOF {
				// THIS IS WHERE STUFF ACTUALLY RETURNS
				log.Println("stream.Send() returned EOF")
				// sendErr <- struct{}{}
			} else if err != nil {
				// check for "rpc error: code = Internal desc = transport is closing"
				if err.Error() != status.New(codes.Internal, transport.ErrConnClosing.Desc).Err().Error() {
					log.Fatalf("Failed to Send ResponseSpec: %v", err)
				}
				log.Println("stream.Send() returned " + err.Error())
				// sendErr <- struct{}{}
			}

			numRequests = 0
			currentRequest++

		case <-recvErr:

			// restart stream
			stream.CloseSend()
			stream, err := client.StreamingGet(context.Background())
			if err != nil {
				log.Fatalf("%v.StreamingGet(_) = _, %v", client, err)
			}
			defer stream.CloseSend()
			go recvStream(worker, responses, stream, receiving, recvErr)

			numRequests = int64(0)
			currentRequest = int64(0)

		case _, more := <-sending:
			if more {
				// we should never get here
				log.Fatalf("Invalid state: message sent on sending channel")
			}

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
	requestRatioM int64,
	requestRatioN int64,
	r *rand.Rand,
	driver <-chan struct{},
	responses chan<- *MeasuredResponse,
) {

	latencyMap := latencyDistribution.ToMap()
	lengthMap := lengthDistribution.ToMap()

	for {
		log.Println("FOO1")
		sending := make(chan struct{})
		// receiving := make(chan struct{})
		sendErr := make(chan struct{})
		log.Println("FOO2")
		go sendStream(driver, lengthMap, latencyMap, errorRate, requestRatioM, requestRatioN, sending, sendErr, worker, client, responses)
		log.Println("FOO3")

		select {
		case <-sendErr:
			log.Println("sendErr")
			// close(receiving)
			// stream.CloseSend()
			log.Println("stream.CloseSend()")
		case <-shutdown:
			log.Println("<-shutdown")
			// stream.CloseSend()
			close(sending)
			// close(receiving)
			return
		}
	}
}

func connect(
	mainWait *sync.WaitGroup,
	cfg Config,
	requestRatioM int64,
	requestRatioN int64,
	connOpts []grpc.DialOption,
	c uint,
	shutdowns []chan struct{},
	lengthDistribution distribution.Distribution,
	latencyDistribution distribution.Distribution,
	driver <-chan struct{},
	responses chan<- *MeasuredResponse,
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
					lengthDistribution, latencyDistribution, float32(cfg.ErrorRate), r, driver, responses)
			} else {
				sendStreamingRequests(worker, client, shutdown,
					lengthDistribution, latencyDistribution, float32(cfg.ErrorRate), requestRatioM, requestRatioN, r, driver, responses)
			}
		}()
	}

	connWait.Wait()
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

	latencyDivisor := int64(1000000)
	if cfg.LatencyUnit == "ms" {
		latencyDivisor = 1000000
	} else if cfg.LatencyUnit == "us" {
		latencyDivisor = 1000
	} else if cfg.LatencyUnit == "ns" {
		latencyDivisor = 1
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

	cleanup := make(chan struct{}, 2)
	interrupt := make(chan os.Signal, 2)
	signal.Notify(interrupt, syscall.SIGINT)

	var bytes, totalBytes, count, sendCount, recvCount, good, totalGood, bad, totalBad, max, min uint
	min = math.MaxInt64
	latencyHist := hdrhistogram.New(0, DayInMillis, 3)
	globalLatencyHist := hdrhistogram.New(0, DayInMillis, 3)
	jitterHist := hdrhistogram.New(0, DayInMillis, 3)
	globalJitterHist := hdrhistogram.New(0, DayInMillis, 3)

	concurrency := cfg.Connections * cfg.Streams

	// By default, allow enough capacity for each worker.
	capacity := concurrency

	if cfg.TotalTargetRps > 0 {
		// Allow several seconds worth of additional capacity in case things back up.
		capacity = cfg.TotalTargetRps * 10
	}

	// Items are sent to this channel to inform sending goroutines when to do work.
	driver := make(chan struct{}, capacity)

	responses := make(chan *MeasuredResponse, capacity)

	requestRatioM, requestRatioN := parseStreamingRatio(cfg.StreamingRatio)
	totalResponses := cfg.TotalRequests * uint(requestRatioN) / uint(requestRatioM)

	// If a target rps is specified, it will periodically notify the driving/reporting
	// goroutine that a full concurrency of work should be driven.
	var driveTick <-chan time.Time
	if cfg.TotalTargetRps > 0 {
		driveTick = time.Tick(time.Second)
	}

	// Drive a single request.
	drive := func() {
		if cfg.TotalRequests > 0 && sendCount == cfg.TotalRequests {
			fmt.Println("drive: done")
			return
		}

		if len(driver) == cap(driver) {
			return
		}

		sendCount++
		fmt.Printf("drive: signal %d %d/%d\n", sendCount, len(driver), cap(driver))

		driver <- struct{}{}
	}

	intervalReport := time.Tick(cfg.Interval)
	previousinterval := time.Now()

	if cfg.MetricAddr != "" {
		registerMetrics()
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.ListenAndServe(cfg.MetricAddr, nil)
		}()
	}

	var mainWait sync.WaitGroup
	mainWait.Add(int(cfg.Connections))

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

	for c := uint(0); c < cfg.Connections; c++ {
		c := c
		shutdowns := shutdownChannels[c]

		go connect(
			&mainWait,
			cfg,
			requestRatioM,
			requestRatioN,
			connOpts,
			c,
			shutdowns,
			lengthDistribution,
			latencyDistribution,
			driver,
			responses,
		)
	}

	go func() {
		mainWait.Add(1)

		// If there is no target RPS, ensure there's enough capacity for all threads to
		// operate:
		if cfg.TotalTargetRps == 0 {
			for i := uint(0); i != concurrency; i++ {
				drive()
			}
		}

		for {
			select {
			case <-interrupt:
				cleanup <- struct{}{}

			case <-cleanup:
				//fmt.Println("cleanup")
				for _, streams := range shutdownChannels {
					for _, shutdown := range streams {
						shutdown <- struct{}{}
					}
				}

				if !cfg.NoIntervalReport && count > 0 {
					t := previousinterval.Add(cfg.Interval)
					logIntervalReport(t, &cfg.Interval, good, bad, bytes, min, max,
						latencyHist, jitterHist)
				}

				if !cfg.NoFinalReport {
					logFinalReport(totalGood, totalBad, totalBytes, globalLatencyHist, globalJitterHist)
				}
				mainWait.Done()
				return

			case <-driveTick:
				fmt.Printf("DRIVETICK\n")
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

				fmt.Printf("drive: %d [%d:%d]\n", n, len(driver), cap(driver))
				for i := uint(0); i != n; i++ {
					drive()
				}

			case resp := <-responses:
				count++
				recvCount++
				promRequests.Inc()

				fmt.Printf("recv %d from %v\n", recvCount, resp.worker)
				if resp.err != nil {
					bad++
					totalBad++
				} else {
					good++
					totalGood++
					promSuccesses.Inc()

					bytes += resp.bytes
					totalBytes += resp.bytes

					latency := resp.latency.Nanoseconds() / latencyDivisor
					if latency < int64(min) {
						min = uint(latency)
					}
					if latency > int64(max) {
						max = uint(latency)
					}
					latencyHist.RecordValue(latency)
					globalLatencyHist.RecordValue(latency)
					promLatencyHistogram.Observe(float64(latency))

					jitter := resp.timeBetweenFrames.Nanoseconds() / latencyDivisor
					promJitterHistogram.Observe(float64(jitter))

					jitterHist.RecordValue(jitter)
					globalJitterHist.RecordValue(jitter)
				}

				// THIS SHOULD ACCOUNT FOR STREAMING RATIO
				// DRIVER SHOULD STOP UNTIL STREAMING REQUEST IS RE_CONNECTECTED
				fmt.Printf("sendCount: %d recvCount: %d totalResponses: %d\n", sendCount, recvCount, totalResponses)
				if recvCount == totalResponses {
					// N.B. recvCount > 0, so totalRequests >0
					cleanup <- struct{}{}
				} else if cfg.TotalTargetRps == 0 {
					// If there's no target rps, just restore capacity immediately.

					// TRY NOT TO USE DRIVER
					// time.Sleep(1000 * time.Millisecond)
					drive()
				}

			case t := <-intervalReport:
				if cfg.NoIntervalReport {
					continue
				}
				logIntervalReport(t, &cfg.Interval, good, bad, bytes, min, max,
					latencyHist, jitterHist)
				bytes, count, good, bad, max, min = 0, 0, 0, 0, 0, math.MaxInt64
				latencyHist.Reset()
				jitterHist.Reset()
				previousinterval = t
			}
		}
	}()

	mainWait.Wait()
}
