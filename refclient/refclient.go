package refclient

import (
	"io"

	pb "github.com/buoyantio/strest-grpc/protos"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"net/http"
	_ "net/http/pprof"
)

func Run() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	address := "localhost:11111"

	log.Infof("connecting to %s", address)

	connOpts := []grpc.DialOption{grpc.WithInsecure()}
	conn, err := grpc.Dial(address, connOpts...)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	client := pb.NewResponderClient(conn)

	stream, err := client.StreamingGet(context.Background())
	if err != nil {
		log.Fatalf("%v.StreamingGet(_) = _, %v", client, err)
	}

	count := -1

	receiving := make(chan struct{})
	go func(stream pb.Responder_StreamingGetClient, receiving chan<- struct{}, count int) {
		received := 0
		for {
			_, err := stream.Recv()
			if err == nil {
				received++
			} else if err == io.EOF {
				if received != count {
					log.Error("stream.Recv() returned unexpected io.EOF")
				}
				break
			} else if err != nil {
				log.Errorf("stream.Recv() returned unexpected error: %s", err)
				break
			}
		}

		close(receiving)
	}(stream, receiving, count)

	err = stream.Send(&pb.StreamingResponseSpec{
		Count: int32(count),
	})
	if err != nil {
		log.Errorf("stream.Send returned %s", err)
	}

	<-receiving

	err = stream.CloseSend()
	if err != nil {
		log.Errorf("stream.CloseSend() returned %s", err)
	}
}
