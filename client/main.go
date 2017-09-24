package main

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
	"google.golang.org/grpc/credentials"
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
	receiver chan<- *MeasuredResponse) {
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
		receiver <- &MeasuredResponse{err: err}
		return
	}

	bytes := uint(len([]byte(resp.Body)))
	latency := time.Since(start)
	//fmt.Printf("%v: rsp %v\n", worker, latency)
	receiver <- &MeasuredResponse{worker: worker, latency: latency, bytes: bytes}
}

func sendNonStreamingRequests(worker workerID,
	client pb.ResponderClient,
	shutdown <-chan struct{},
	clientTimeout time.Duration,
	lengthDistribution distribution.Distribution,
	latencyDistribution distribution.Distribution,
	errorRate float32, r *rand.Rand,
	driver <-chan struct{},
	results chan<- *MeasuredResponse) {
	for {
		select {
		case <-shutdown:
			return
		case <-driver:
			safeNonStreamingRequest(worker, client, clientTimeout, lengthDistribution, latencyDistribution, errorRate, r, results)
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

func sendStreamingRequests(worker workerID,
	client pb.ResponderClient,
	shutdown <-chan struct{},
	lengthDistribution, latencyDistribution distribution.Distribution,
	streamingRatio string,
	r *rand.Rand,
	driver <-chan struct{},
	results chan<- *MeasuredResponse) error {

	stream, err := client.StreamingGet(context.Background())
	if err != nil {
		log.Fatalf("%v.StreamingGet(_) = _, %v", client, err)
	}
	defer stream.CloseSend()

	latencyMap := latencyDistribution.ToMap()
	lengthMap := lengthDistribution.ToMap()

	waitc := make(chan struct{})
	go func() {
		for {
			select {
			case <-driver:
				start := time.Now()
				resp, err := stream.Recv()
				elapsed := time.Since(start)

				if err == io.EOF {
					// read done.
					close(waitc)
					return
				}
				if err != nil {
					fmt.Println("streaming read failed: ", err)
					return
				}

				timeBetweenFrames := time.Duration(resp.CurrentFrameSent - resp.LastFrameSent)
				bytes := uint(len([]byte(resp.Body)))
				results <- &MeasuredResponse{
					worker,
					timeBetweenFrames,
					elapsed,
					bytes,
					err,
				}
			}
		}
	}()

	requestRatioM, requestRatioN := parseStreamingRatio(streamingRatio)
	numRequests := int64(0)
	currentRequest := int64(0)
	for {
		select {
		case <-shutdown:
			return nil
		default:
			if (currentRequest % requestRatioM) == 0 {
				numRequests = requestRatioN
			}

			err := stream.Send(&pb.StreamingResponseSpec{
				Count:              int32(numRequests),
				LatencyPercentiles: latencyMap,
				LengthPercentiles:  lengthMap,
			})

			if err != nil {
				log.Fatalf("Failed to Send ResponseSpec: %v", err)
			}

			numRequests = 0
			currentRequest++
		}
	}
}

func main() {
	var (
		address               = flag.String("address", "localhost:11111", "address of strest-grpc service or intermediary")
		useUnixAddr           = flag.Bool("unix", false, "use Unix Domain Sockets instead of TCP")
		clientTimeout         = flag.Duration("clientTimeout", 0, "timeout for unary client requests. Default: no timeout")
		connections           = flag.Uint("connections", 1, "number of concurrent connections")
		streams               = flag.Uint("streams", 1, "number of concurrent streams per connection")
		totalRequests         = flag.Uint("totalRequests", 0, "total number of requests to send. default: infinite")
		totalTargetRps        = flag.Uint("totalTargetRps", 0, "target requests per second")
		interval              = flag.Duration("interval", 10*time.Second, "reporting interval")
		latencyPercentileFlag = flag.String("latencyPercentiles", "100=0", "response latency percentile distribution. (e.g. 50=10,100=100)")
		lengthPercentileFlag  = flag.String("lengthPercentiles", "100=0", "response body length percentile distribution. (e.g. 50=100,100=1000)")
		errorRate             = flag.Float64("errorRate", 0.0, "the chance to return an error")
		noFinalReport         = flag.Bool("noFinalReport", false, "do not print a final JSON output report")
		noIntervalReport      = flag.Bool("noIntervalReport", false, "only print the final report, nothing intermediate")
		streaming             = flag.Bool("streaming", false, "use the streaming features of strest server")
		streamingRatio        = flag.String("streamingRatio", "1:1", "the ratio of streaming requests/responses")
		metricAddr            = flag.String("metricAddr", "", "address to serve metrics on")
		latencyUnit           = flag.String("latencyUnit", "ms", "latency units [ms|us|ns]")
		tlsTrustChainFile     = flag.String("tlsTrustChainFile", "", "the path to the certificate used to validate the remote's signature")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}

	flag.Parse()

	latencyDivisor := int64(1000000)
	if *latencyUnit == "ms" {
		latencyDivisor = 1000000
	} else if *latencyUnit == "us" {
		latencyDivisor = 1000
	} else if *latencyUnit == "ns" {
		latencyDivisor = 1
	} else {
		log.Fatalf("latency unit should be [ms | us | ns].")
	}

	if *noIntervalReport && *noFinalReport {
		log.Fatalf("cannot use both -noIntervalReport and -noFinalReport.")
	}

	if *connections < 1 {
		exUsage("connections must be at least 1")
	}

	if *streams < 1 {
		exUsage("streams must be at least 1")
	}

	latencyPercentiles, err := percentiles.ParsePercentiles(*latencyPercentileFlag)
	if err != nil {
		log.Fatalf("latencyPercentiles was not valid: %v", err)
	}

	latencyDistribution, err := distribution.FromMap(latencyPercentiles)
	if err != nil {
		log.Fatalf("unable to create latency distribution: %v", err)
	}

	lengthPercentiles, err := percentiles.ParsePercentiles(*lengthPercentileFlag)
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

	concurrency := *connections * *streams

	// By default, allow enough capacity for each worker.
	capacity := concurrency

	if *totalTargetRps > 0 {
		// Allow several seconds worth of additional capacity in case things back up.
		capacity = *totalTargetRps * 10
	}

	// Items are sent to this channel to inform sending goroutines when to do work.
	driver := make(chan struct{}, capacity)

	receiver := make(chan *MeasuredResponse, capacity)

	// If a target rps is specified, it will periodically notify the driving/reporting
	// goroutine that a full concurrency of work should be driven.
	var driveTick <-chan time.Time
	if *totalTargetRps > 0 {
		driveTick = time.Tick(time.Second)
	}

	// Drive a single request.
	drive := func() {
		if *totalRequests > 0 && sendCount == *totalRequests {
			//fmt.Println("drive: done")
			return
		}

		if len(driver) == cap(driver) {
			return
		}

		sendCount++
		//fmt.Printf("drive: signal %d %d/%d\n", sendCount, len(driver), cap(driver))

		driver <- struct{}{}
	}

	intervalReport := time.Tick(*interval)
	previousInterval := time.Now()

	if *metricAddr != "" {
		registerMetrics()
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.ListenAndServe(*metricAddr, nil)
		}()
	}

	var mainWait sync.WaitGroup
	mainWait.Add(int(*connections))

	shutdownChannels := make([][]chan struct{}, *connections)
	for c := uint(0); c < *connections; c++ {
		shutdowns := make([]chan struct{}, *streams)
		for s := uint(0); s < *streams; s++ {
			shutdowns[s] = make(chan struct{})
		}

		shutdownChannels[c] = shutdowns
	}

	var connOpts []grpc.DialOption
	if *tlsTrustChainFile != "" {
		creds, err := credentials.NewClientTLSFromFile(*tlsTrustChainFile, "")
		if err != nil {
			log.Fatalf("invalid ca cert file: %v", err)
		}
		connOpts = append(connOpts, grpc.WithTransportCredentials(creds))
	} else {
		connOpts = append(connOpts, grpc.WithInsecure())
	}

	af := "tcp"
	if *useUnixAddr {
		af = "unix"
	}
	dial := func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(af, addr, timeout)
	}
	connOpts = append(connOpts, grpc.WithDialer(dial))

	for c := uint(0); c < *connections; c++ {
		c := c
		shutdowns := shutdownChannels[c]

		go func() {
			defer mainWait.Done()

			var connWait sync.WaitGroup
			connWait.Add(int(*streams))

			conn, err := grpc.Dial(*address, connOpts...)
			if err != nil {
				log.Fatalf("did not connect: %v", err)
			}
			defer conn.Close()
			client := pb.NewResponderClient(conn)

			for s := uint(0); s < *streams; s++ {
				worker := workerID{connection: c, stream: s}
				shutdown := shutdowns[s]

				go func() {
					defer connWait.Done()

					r := rand.New(rand.NewSource(time.Now().UnixNano()))
					if !*streaming {
						sendNonStreamingRequests(worker, client, shutdown, *clientTimeout,
							lengthDistribution, latencyDistribution, float32(*errorRate), r, driver, receiver)
					} else {
						err := sendStreamingRequests(worker, client, shutdown,
							lengthDistribution, latencyDistribution, *streamingRatio, r, driver, receiver)
						if err != nil {
							log.Fatalf("could not send a request: %v", err)
						}
					}
				}()
			}

			connWait.Wait()
		}()
	}

	go func() {
		mainWait.Add(1)

		// If there is no target RPS, ensure there's enough capacity for all threads to
		// operate:
		if *totalTargetRps == 0 {
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

				if !*noIntervalReport && count > 0 {
					t := previousInterval.Add(*interval)
					logIntervalReport(t, interval, good, bad, bytes, min, max,
						latencyHist, jitterHist)
				}

				if !*noFinalReport {
					logFinalReport(totalGood, totalBad, totalBytes, globalLatencyHist, globalJitterHist)
				}
				mainWait.Done()
				return

			case <-driveTick:
				// When a target rps is set, it may fire several times a second so that a
				// full concurrency of work may be scheduled at each interval.

				n := *totalTargetRps

				if *totalRequests > 0 {
					r := *totalRequests - sendCount
					if r < n {
						n = r
					}
				}

				r := uint(cap(driver) - len(driver))
				if r < n {
					n = r
				}

				//fmt.Printf("drive: %d [%d:%d]\n", n, len(driver), cap(driver))
				for i := uint(0); i != n; i++ {
					drive()
				}

			case resp := <-receiver:
				count++
				recvCount++
				promRequests.Inc()

				//fmt.Printf("recv %d from %v\n", recvCount, resp.worker)
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

				if recvCount == *totalRequests {
					// N.B. recvCount > 0, so totalRequests >0
					cleanup <- struct{}{}
				} else if *totalTargetRps == 0 {
					// If there's no target rps, just restore capacity immediately.
					drive()
				}

			case t := <-intervalReport:
				if *noIntervalReport {
					continue
				}
				logIntervalReport(t, interval, good, bad, bytes, min, max,
					latencyHist, jitterHist)
				bytes, count, good, bad, max, min = 0, 0, 0, 0, 0, math.MaxInt64
				latencyHist.Reset()
				jitterHist.Reset()
				previousInterval = t
			}
		}
	}()

	mainWait.Wait()
	os.Exit(0)
}
