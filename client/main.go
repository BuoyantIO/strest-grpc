package main

import (
	pb "../protos"
	"flag"
	"fmt"
	"github.com/buoyantio/strest-grpc/client/distribution"
	"github.com/buoyantio/strest-grpc/client/percentiles"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"
	"time"
)

const (
	address     = "localhost:11111"
	defaultName = "world"
)

func exUsage(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", msg)
	fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
	flag.PrintDefaults()
	os.Exit(64)
}

func main() {
	concurrency := flag.Int("concurrency", 1, "client concurrency level")
	requests := flag.Int("requests", 100, "number of requests per connection")
	latencyPercentileFlag := flag.String("latencyPercentiles", "0=10,100=1000", "response latency percentile distribution.")
	lengthPercentileFlag := flag.String("lengthPercentiles", "0=100,100=10000", "response body length percentile distribution.")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}

	flag.Parse()

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

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	cleanup := make(chan os.Signal)
	signal.Notify(cleanup, syscall.SIGINT)

	var wg sync.WaitGroup
	wg.Add(*concurrency)

	for i := int(0); i < *concurrency; i++ {
		go func() {
			// Set up a connection to the server.
			conn, err := grpc.Dial(address, grpc.WithInsecure())
			if err != nil {
				log.Fatalf("did not connect: %v", err)
			}
			defer conn.Close()
			c := pb.NewResponderClient(conn)

			for j := int(0); j < *requests; j++ {
				lengthValue := lengthDistribution.Get(r.Int() % 1000)
				latencyValue := latencyDistribution.Get(r.Int() % 1000)
				r, err := c.Get(context.Background(),
					&pb.ResponseSpec{
						Count:   1,
						Length:  int32(lengthValue),
						Latency: latencyValue})
				if err != nil {
					log.Fatalf("could not send a request: %v", err)
				}
				log.Printf("Response: %s", r.Body)
			}
			wg.Done()
		}()
	}

	go func() {
		for {
			select {
			case <-cleanup:
				go func() {
					os.Exit(1)
				}()
			}
		}
	}()

	wg.Wait()
}
