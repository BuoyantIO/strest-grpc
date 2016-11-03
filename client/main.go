package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "../protos"
	"github.com/buoyantio/strest-grpc/client/distribution"
	"github.com/buoyantio/strest-grpc/client/percentiles"
	"github.com/codahale/hdrhistogram"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// MeasuredResponse tracks the latency of a response and any
// accompaning error. timeBetweenFrames is how long we spend waiting
// for the next frame to arrive.
type MeasuredResponse struct {
	timeBetweenFrames time.Duration
	latency           time.Duration
	bytes             int64
	err               error
}

func exUsage(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", msg)
	fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
	flag.PrintDefaults()
	os.Exit(64)
}

// Report provides top-level good/bad numbers and a latency breakdown.
type Report struct {
	Good    int64      `json:"good"`
	Bad     int64      `json:"bad"`
	Bytes   int64      `json:"bytes"`
	Latency *Quantiles `json:"latency"`
	Jitter  *Quantiles `json:"jitter"`
}

// Latency Percentiles
type Quantiles struct {
	Quantile50  int64 `json:"50"`
	Quantile95  int64 `json:"95"`
	Quantile99  int64 `json:"99"`
	Quantile999 int64 `json:"999"`
}

// 1 day in milliseconds
const DAY_IN_MS int64 = 24 * 60 * 60 * 1000000

var sizeSuffixes = []string{"B", "KB", "MB", "GB"}

func formatBytes(bytes int64) string {
	sz := float64(bytes)
	order := 0
	for order < len(sizeSuffixes) && sz >= 1024 {
		sz = sz / float64(1024)
		order += 1
	}
	return fmt.Sprintf("%0.1f%s", sz, sizeSuffixes[order])
}

// Periodically print stats about the request load.
func logIntervalReport(
	now time.Time,
	interval *time.Duration,
	good, bad, bytes, min, max int64,
	latencyHist *hdrhistogram.Histogram,
	jitterHist *hdrhistogram.Histogram) {
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

func logFinalReport(good, bad, bytes int64, latencies *hdrhistogram.Histogram, jitters *hdrhistogram.Histogram) {
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

func sendNonStreamingRequests(client pb.ResponderClient,
	requests int64, lengthDistribution distribution.Distribution,
	latencyDistribution distribution.Distribution, r *rand.Rand,
	received chan *MeasuredResponse) error {
	for j := int64(0); j < requests; j++ {
		start := time.Now()
		resp, err := client.Get(context.Background(),
			&pb.ResponseSpec{
				Length:  int32(lengthDistribution.Get(r.Int31() % 1000)),
				Latency: latencyDistribution.Get(r.Int31() % 1000)})

		bytes := int64(len([]byte(resp.Body)))
		received <- &MeasuredResponse{0, time.Since(start), bytes, err}

		if err != nil {
			return err
		}
	}

	return nil
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

func sendStreamingRequests(client pb.ResponderClient,
	requests int64, lengthDistribution distribution.Distribution,
	latencyDistribution distribution.Distribution, streamingRatio string,
	r *rand.Rand, received chan *MeasuredResponse) error {
	stream, err := client.StreamingGet(context.Background())
	if err != nil {
		log.Fatalf("%v.StreamingGet(_) = _, %v", client, err)
	}

	latencyMap := latencyDistribution.ToMap()
	lengthMap := lengthDistribution.ToMap()

	waitc := make(chan struct{})
	go func() {
		for {
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

			bytes := int64(len([]byte(resp.Body)))
			received <- &MeasuredResponse{
				timeBetweenFrames,
				elapsed,
				bytes,
				err,
			}
		}
	}()

	requestRatioM, requestRatioN := parseStreamingRatio(streamingRatio)
	var numRequests = int64(0)
	for j := int64(0); j < requests; j++ {
		if (j % requestRatioM) == 0 {
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
	}

	stream.CloseSend()
	<-waitc
	return nil
}

func main() {
	var (
		address               = flag.String("address", "localhost:11111", "hostname:port of strest-grpc service or intermediary")
		concurrency           = flag.Int("concurrency", 1, "client concurrency level")
		requests              = flag.Int64("requests", 10000, "number of requests per connection")
		interval              = flag.Duration("interval", 10*time.Second, "reporting interval")
		latencyPercentileFlag = flag.String("latencyPercentiles", "50=10,100=100", "response latency percentile distribution.")
		lengthPercentileFlag  = flag.String("lengthPercentiles", "50=100,100=1000", "response body length percentile distribution.")
		disableFinalReport    = flag.Bool("disableFinalReport", false, "do not print a final JSON output report")
		onlyFinalReport       = flag.Bool("onlyFinalReport", false, "only print the final report, nothing intermediate")
		streaming             = flag.Bool("streaming", false, "use the streaming features of strest server")
		streamingRatio        = flag.String("streamingRatio", "1:1", "the ratio of streaming requests/responses")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}

	flag.Parse()

	if *onlyFinalReport && *disableFinalReport {
		log.Fatalf("cannot use both -onlyFinalReport and -disableFinalReport.")
	}

	if *concurrency < 1 {
		exUsage("concurrency must be at least 1")
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

	cleanup := make(chan os.Signal)
	signal.Notify(cleanup, syscall.SIGINT)

	var bytes, totalBytes, count, totalCount, good, totalGood, bad, totalBad, max, min int64
	min = math.MaxInt64
	latencyHist := hdrhistogram.New(0, DAY_IN_MS, 3)
	globalLatencyHist := hdrhistogram.New(0, DAY_IN_MS, 3)
	jitterHist := hdrhistogram.New(0, DAY_IN_MS, 3)
	globalJitterHist := hdrhistogram.New(0, DAY_IN_MS, 3)
	received := make(chan *MeasuredResponse, 10000)

	var timeout = make(<-chan time.Time)
	if !*onlyFinalReport {
		timeout = time.After(*interval)
	}

	var wg sync.WaitGroup
	wg.Add(*concurrency)

	for i := int(0); i < *concurrency; i++ {
		go func() {
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			// Set up a connection to the server.
			conn, err := grpc.Dial(*address, grpc.WithInsecure())
			if err != nil {
				log.Fatalf("did not connect: %v", err)
			}
			defer conn.Close()
			client := pb.NewResponderClient(conn)

			if !*streaming {
				err := sendNonStreamingRequests(client,
					*requests, lengthDistribution, latencyDistribution, r, received)
				if err != nil {
					log.Fatalf("could not send a request: %v", err)
				}
			} else {
				err := sendStreamingRequests(client,
					*requests, lengthDistribution, latencyDistribution, *streamingRatio, r, received)
				if err != nil {
					log.Fatalf("could not send a request: %v", err)
				}
			}

			wg.Done()
		}()
	}

	go func() {
		for {
			select {
			case <-cleanup:
				// FIX: how can we close the client
				// connection properly here?
				if !*disableFinalReport {
					logFinalReport(totalGood, totalBad, totalBytes, globalLatencyHist, globalJitterHist)
				}
				os.Exit(0)

			case resp := <-received:
				count++
				totalCount++
				if resp.err != nil {
					bad++
					totalBad++
				} else {
					good++
					totalGood++

					bytes += resp.bytes
					totalBytes += resp.bytes

					latency := resp.latency.Nanoseconds() / 1000000
					if latency < min {
						min = latency
					}
					if latency > max {
						max = latency
					}
					latencyHist.RecordValue(latency)
					globalLatencyHist.RecordValue(latency)

					jitter := resp.timeBetweenFrames.Nanoseconds() / 1000000
					jitterHist.RecordValue(jitter)
					globalJitterHist.RecordValue(jitter)
				}

			case t := <-timeout:
				if min == math.MaxInt64 {
					min = 0
				}
				logIntervalReport(t, interval, good, bad, bytes, min, max,
					latencyHist, jitterHist)
				bytes, count, good, bad, max, min = 0, 0, 0, 0, 0, math.MaxInt64
				latencyHist.Reset()
				jitterHist.Reset()
				timeout = time.After(*interval)
			}
		}
	}()

	wg.Wait()
	if !*disableFinalReport {
		logFinalReport(totalGood, totalBad, totalBytes, globalLatencyHist, globalJitterHist)
	}
}
