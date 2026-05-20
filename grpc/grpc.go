package grpc

import (
	"archangel/config"
	pb "archangel/proto"
	"time"

	"github.com/rs/zerolog/log"
)

// LogServer implements the protobuf proto.LogServiceServer streaming API.
type LogServer struct {
	pb.UnimplementedLogServiceServer
}

// StreamLogs reads logs from the global broadcaster and streams them down to gRPC clients.
func (s *LogServer) StreamLogs(req *pb.LogRequest, stream pb.LogService_StreamLogsServer) error {
	log.Info().Str("min_level", req.GetMinLevel()).Msg("gRPC logging stream client established subscription.")

	// Register with config.LogBroadcaster
	ch := config.LogBroadcaster.Register()
	defer config.LogBroadcaster.Deregister(ch)

	for {
		select {
		case <-stream.Context().Done():
			log.Info().Msg("gRPC logging stream client closed subscription.")
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}

			line := &pb.LogLine{
				Timestamp:   time.Now().Unix(),
				Level:       "info", // Can be parsed if using JSON format
				Message:     msg,
				PayloadJson: "",
			}

			if err := stream.Send(line); err != nil {
				log.Error().Err(err).Msg("Failed to dispatch log line over gRPC transport stream.")
				return err
			}
		}
	}
}
