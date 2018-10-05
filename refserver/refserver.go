package refserver

import (
	"errors"
	"io"
	"net"

	pb "github.com/buoyantio/strest-grpc/protos"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func Run() {
	address := ":11111"

	log.Infof("starting gRPC server on %s\n", address)

	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("listening on tcp:%s: %v", address, err)
	}

	opts := []grpc.ServerOption{}
	s := grpc.NewServer(opts...)

	pb.RegisterResponderServer(s, &refserver{})
	s.Serve(lis)
}

type refserver struct{}

func (*refserver) Get(ctx context.Context, in *pb.ResponseSpec) (*pb.ResponseReply, error) {
	return &pb.ResponseReply{}, grpc.Errorf(codes.OK, "Error")
}

func (*refserver) StreamingGet(stream pb.Responder_StreamingGetServer) error {
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

		for i := int32(0); i < spec.Count; i++ {
			if err := stream.Send(&pb.ResponseReply{}); err != nil {
				log.Errorf("stream.Send returned %s", err)
				return err
			}
		}
	}
}
