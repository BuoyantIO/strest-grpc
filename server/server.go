package server

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
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

// Configuration for a server
type Config struct {
	Address        string
	UseUnixAddr    bool
	MetricAddr     string
	TLSCertFile    string
	TLSPrivKeyFile string
}

func (cfg *Config) serveMetrics() {
	if cfg.MetricAddr != "" {
		registerMetrics()
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.ListenAndServe(cfg.MetricAddr, nil)
		}()
	}
}

func (cfg *Config) af() string {
	if cfg.UseUnixAddr {
		return "unix"
	} else {
		return "tcp"
	}
}

func (cfg *Config) tlsCreds() (credentials.TransportCredentials, error) {
	if cfg.TLSCertFile != "" && cfg.TLSPrivKeyFile != "" {
		return credentials.NewServerTLSFromFile(cfg.TLSCertFile, cfg.TLSPrivKeyFile)
	}
	return nil, nil
}

// run the server
func (cfg Config) Run() {
	rand.Seed(time.Now().UnixNano())

	cfg.serveMetrics()

	fmt.Println("starting gRPC server on", cfg.Address)

	af := cfg.af()
	lis, err := net.Listen(af, cfg.Address)
	if err != nil {
		log.Fatalf("listening on %s:%s: %v", af, cfg.Address, err)
	}

	var opts []grpc.ServerOption

	creds, err := cfg.tlsCreds()
	if err != nil {
		log.Fatalf("invalid ca cert file: %v", err)
	} else if creds != nil {
		opts = append(opts, grpc.Creds(creds))
	}

	s := grpc.NewServer(opts...)

	pb.RegisterResponderServer(s, new(server))
	s.Serve(lis)
}
