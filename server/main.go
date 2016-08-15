package main

import (
	pb "../protos"
	"errors"
	"github.com/buoyantio/strest-grpc/client/distribution"
	"github.com/buoyantio/strest-grpc/server/random_string"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"io"
	"log"
	"math/rand"
	"net"
	"time"
)

const port = ":11111"

type server struct{}

// Get returns a single response after waiting for in.Count
// milliseconds.
func (s *server) Get(ctx context.Context, in *pb.ResponseSpec) (*pb.ResponseReply, error) {
	var src = rand.NewSource(time.Now().UnixNano())
	if in.Latency > 0 {
		time.Sleep(time.Duration(in.Latency) * time.Millisecond)
	}
	return &pb.ResponseReply{Body: random_string.RandStringBytesMaskImprSrc(src, int(in.Length))}, nil
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

		for i := int32(0); i < spec.Count; i++ {
			timeToSleep := latencyDistribution.Get(r.Int31() % 1000)
			bodySize := lengthDistribution.Get(r.Int31() % 1000)

			if timeToSleep > 0 {
				time.Sleep(time.Duration(timeToSleep) * time.Millisecond)
			}

			response := &pb.ResponseReply{
				Body: random_string.RandStringBytesMaskImprSrc(src, int(bodySize)),
			}

			if err := stream.Send(response); err != nil {
				return err
			}
		}
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	pb.RegisterResponderServer(s, new(server))
	s.Serve(lis)
}
