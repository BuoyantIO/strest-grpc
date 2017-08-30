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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

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
	r := rand.New(src)
	if in.Latency > 0 {
		time.Sleep(time.Duration(in.Latency) * time.Millisecond)
	}
	promRequests.Inc()
	promResponses.Inc()
	promBytesSent.Add(float64(in.Length))
	var statusCode codes.Code
	if r.Float32() < in.ErrorRate {
		statusCode = codes.Unknown
	} else {
		statusCode = codes.OK
	}
	return &pb.ResponseReply{
		Body:             random_string.RandStringBytesMaskImprSrc(src, int(in.Length)),
		LastFrameSent:    0,
		CurrentFrameSent: time.Now().UnixNano(),
	}, grpc.Errorf(statusCode, "Error")
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
	rand.Seed(time.Now().UnixNano())

	var (
		address        = flag.String("address", ":11111", "hostname:port to serve on")
		metricAddr     = flag.String("metricAddr", "", "address to serve metrics on")
		tlsCertFile    = flag.String("tlsCertFile", "", "the path to the trust certificate")
		tlsPrivKeyFile = flag.String("tlsPrivKeyFile", "", "the path to the server's private key")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <url> [flags]\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}

	flag.Parse()

	if *metricAddr != "" {
		registerMetrics()
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.ListenAndServe(*metricAddr, nil)
		}()
	}

	fmt.Println("starting gRPC server on", *address)

	lis, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	var opts []grpc.ServerOption
	if *tlsCertFile != "" && *tlsPrivKeyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(*tlsCertFile, *tlsPrivKeyFile)
		if err != nil {
			log.Fatalf("invalid ca cert file: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	s := grpc.NewServer(opts...)

	pb.RegisterResponderServer(s, new(server))
	s.Serve(lis)
}
