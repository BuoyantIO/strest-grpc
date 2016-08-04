package main

import (
	pb "../protos"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/buoyantio/strest-grpc/client/distribution"
	"github.com/buoyantio/strest-grpc/client/percentiles"
	"github.com/codahale/hdrhistogram"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"
	"time"
)

// MeasuredResponse tracks the latency of a response and any accompaning error.
type MeasuredResponse struct {
	latency time.Duration
	err     error
}

func exUsage(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", msg)
	fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
	flag.PrintDefaults()
	os.Exit(64)
}

// Report provides top-level good/bad/failed numbers and a latency breakdown.
type Report struct {
	Good    int64    `json:"good"`
	Bad     int64    `json:"bad"`
	Failed  int64    `json:"failed"`
	Latency *Latency `json:"latency"`
}

// Latency Percentiles
type Latency struct {
	Quantile50  int64 `json:"50"`
	Quantile95  int64 `json:"95"`
	Quantile99  int64 `json:"99"`
	Quantile999 int64 `json:"999"`
}

func logFinalReport(good int64, bad int64, failed int64, histogram *hdrhistogram.Histogram) {
	latency := Latency{
		Quantile50:  histogram.ValueAtQuantile(50) / 1000000,
		Quantile95:  histogram.ValueAtQuantile(95) / 1000000,
		Quantile99:  histogram.ValueAtQuantile(99) / 1000000,
		Quantile999: histogram.ValueAtQuantile(999) / 1000000,
	}

	report := Report{
		Good:    good,
		Bad:     bad,
		Failed:  failed,
		Latency: &latency,
	}

	if data, err := json.MarshalIndent(report, "", "  "); err != nil {
		log.Fatal("Unable to generate report: ", err)
	} else {
		fmt.Println(string(data))
	}
}

func main() {
	var (
		address               = flag.String("address", "localhost:11111", "hostname:port of strest-grpc service or intermediary")
		concurrency           = flag.Int("concurrency", 1, "client concurrency level")
		requests              = flag.Int("requests", 10000, "number of requests per connection")
		interval              = flag.Duration("interval", 10*time.Second, "reporting interval")
		latencyPercentileFlag = flag.String("latencyPercentiles", "50=10,100=100", "response latency percentile distribution.")
		lengthPercentileFlag  = flag.String("lengthPercentiles", "50=100,100=1000", "response body length percentile distribution.")
		disableFinalReport    = flag.Bool("disableFinalReport", false, "do not print a final JSON output report")
		onlyFinalReport       = flag.Bool("onlyFinalReport", false, "only print the final report, nothing intermediate")
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

	var count, totalCount, good, totalGood, failed, totalFailed, bad, totalBad, max, min int64
	min = math.MaxInt64

	hist := hdrhistogram.New(0, int64(time.Second), 5)
	globalHist := hdrhistogram.New(0, int64(time.Second), 5)
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
			c := pb.NewResponderClient(conn)

			for j := int(0); j < *requests; j++ {
				lengthValue := lengthDistribution.Get(r.Int() % 1000)
				latencyValue := latencyDistribution.Get(r.Int() % 1000)
				start := time.Now()
				_, err := c.Get(context.Background(),
					&pb.ResponseSpec{
						Count:   1,
						Length:  int32(lengthValue),
						Latency: latencyValue})

				received <- &MeasuredResponse{time.Since(start), err}

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
				if !*disableFinalReport {
					logFinalReport(totalGood, totalBad, totalFailed, globalHist)
				}
				os.Exit(1)
			case response := <-received:
				count++
				totalCount++
				if response.err != nil {
					failed++
					totalFailed++
				} else {
					good++
					totalGood++
					latency := response.latency.Nanoseconds()
					if latency < min {
						min = latency
					}

					if latency > max {
						max = latency
					}

					hist.RecordValue(latency)
					globalHist.RecordValue(latency)
				}
			case t := <-timeout:
				if min == math.MaxInt64 {
					min = 0
				}
				// Periodically print stats about the request load.
				fmt.Printf("%s %6d/%1d/%1d requests %s %3d [%3d %3d %3d %4d ] %4d\n",
					t.Format(time.RFC3339),
					good,
					bad,
					failed,
					interval,
					min/1000000,
					hist.ValueAtQuantile(50)/1000000,
					hist.ValueAtQuantile(95)/1000000,
					hist.ValueAtQuantile(99)/1000000,
					hist.ValueAtQuantile(999)/1000000,
					max/1000000)
				count, good, bad, failed, max, min = 0, 0, 0, 0, 0, math.MaxInt64
				hist.Reset()
				timeout = time.After(*interval)
			}
		}
	}()

	wg.Wait()
	if !*disableFinalReport {
		logFinalReport(totalGood, totalBad, totalFailed, globalHist)
	}
}
