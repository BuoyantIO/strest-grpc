package main

import (
	pb "../protos"
	"github.com/buoyantio/strest-grpc/server/random_string"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"log"
	"math/rand"
	"net"
	"time"
)

const port = ":11111"

type server struct{}

func (s *server) Get(ctx context.Context, in *pb.ResponseSpec) (*pb.ResponseReply, error) {
	var src = rand.NewSource(time.Now().UnixNano())
	time.Sleep(time.Duration(in.Latency) * time.Millisecond)
	return &pb.ResponseReply{Body: random_string.RandStringBytesMaskImprSrc(src, int(in.Length))}, nil
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
