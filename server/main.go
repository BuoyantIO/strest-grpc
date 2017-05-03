package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/buoyantio/strest-grpc/client/distribution"
	pb "github.com/buoyantio/strest-grpc/protos"
	"github.com/buoyantio/strest-grpc/server/random_string"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const port = ":11111"

type server struct{}

var (
	promRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "requests",
		Help: "Number of requests",
	})

	promResponses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "responses",
		Help: "Number of responses sent",
	})

	promBytesSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bytes_tx",
		Help: "Number of bytes sent",
	})
	promStreamErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "stream_errors",
		Help: "Number of streaming errors seen",
	})
)

func registerMetrics() {
	prometheus.MustRegister(promRequests)
	prometheus.MustRegister(promResponses)
	prometheus.MustRegister(promBytesSent)
	prometheus.MustRegister(promStreamErrors)

}

// Get returns a single response after waiting for in.Count
// milliseconds.
func (s *server) Get(ctx context.Context, in *pb.ResponseSpec) (*pb.ResponseReply, error) {
	var src = rand.NewSource(time.Now().UnixNano())
	if in.Latency > 0 {
		time.Sleep(time.Duration(in.Latency) * time.Millisecond)
	}
	promRequests.Inc()
	promResponses.Inc()
	promBytesSent.Add(float64(in.Length))
	return &pb.ResponseReply{
		Body:             random_string.RandStringBytesMaskImprSrc(src, int(in.Length)),
		LastFrameSent:    0,
		CurrentFrameSent: time.Now().UnixNano(),
	}, nil
}

// StreamingGet implements a streaming request/response.
// The ResponseSpec specifies how many and what size/latency responses
// to send.
func (s *server) StreamingGet(stream pb.Responder_StreamingGetServer) error {
	var src = rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)

	for {
		spec, err := stream.Recv()

		if err == io.EOF {
			return nil
		}

		if err != nil {
			promStreamErrors.Inc()
			return err
		}

		if spec.Count < 0 {
			return errors.New("invalid ResponseSpec, Count cannot be negative")
		}

		if spec.Count == 0 {
			return nil
		}

		latencyDistribution, err := distribution.FromMap(spec.LatencyPercentiles)
		if err != nil {
			return err
		}

		lengthDistribution, err := distribution.FromMap(spec.LengthPercentiles)
		if err != nil {
			return err
		}

		lastFrameSent := time.Now().UnixNano()
		promRequests.Inc()
		for i := int32(0); i < spec.Count; i++ {
			timeToSleep := latencyDistribution.Get(r.Int31() % 1000)
			bodySize := lengthDistribution.Get(r.Int31() % 1000)
			promBytesSent.Add(float64(bodySize))

			if timeToSleep > 0 {
				time.Sleep(time.Duration(timeToSleep) * time.Millisecond)
			}

			now := time.Now().UnixNano()
			promResponses.Inc()
			response := &pb.ResponseReply{
				Body:             random_string.RandStringBytesMaskImprSrc(src, int(bodySize)),
				LastFrameSent:    lastFrameSent,
				CurrentFrameSent: now,
			}
			lastFrameSent = now

			if err := stream.Send(response); err != nil {
				promStreamErrors.Inc()
				return err
			}
		}
	}
}

func main() {
	help := flag.Bool("help", false, "show help message")
	metricAddr := flag.String("metric-addr", "", "address to serve metrics on")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <url> [flags]\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}

	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(64)
	}

	if *metricAddr != "" {
		registerMetrics()
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.ListenAndServe(*metricAddr, nil)
		}()
	}
	rand.Seed(time.Now().UnixNano())
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	pb.RegisterResponderServer(s, new(server))
	s.Serve(lis)
}
