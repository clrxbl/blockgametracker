package queue

import (
	flatbuffers "github.com/google/flatbuffers/go"

	pb "mcstatus-exporter/blockgametracker_protocol/ping"
	"mcstatus-exporter/internal/ping"
)

func platformFor(edition string) pb.Platform {
	switch edition {
	case ping.EditionJava:
		return pb.Platformjava
	case ping.EditionBedrock:
		return pb.Platformbedrock
	default:
		return pb.Platformunknown
	}
}

// EncodeBatch builds a flatbuffers PingBatch root containing the given results.
func EncodeBatch(source string, results []ping.Result) []byte {
	builder := flatbuffers.NewBuilder(1024)

	pingOffsets := make([]flatbuffers.UOffsetT, 0, len(results))
	for _, r := range results {
		serverOff := builder.CreateString(r.Name)
		addrOff := builder.CreateString(r.QueryAddress)
		asNameOff := builder.CreateString(r.ASName)

		pb.PingStart(builder)
		pb.PingAddTime(builder, uint64(r.Time.Unix()))
		pb.PingAddServer(builder, serverOff)
		pb.PingAddServerAddress(builder, addrOff)
		pb.PingAddPlatform(builder, platformFor(r.Edition))
		pb.PingAddAsNumber(builder, r.ASNum)
		pb.PingAddAsName(builder, asNameOff)
		if r.PlayerCount != nil {
			pb.PingAddPlayerCount(builder, uint32(*r.PlayerCount))
		}
		pingOffsets = append(pingOffsets, pb.PingEnd(builder))
	}

	pb.PingBatchStartPingsVector(builder, len(pingOffsets))
	for i := len(pingOffsets) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(pingOffsets[i])
	}
	pingsVec := builder.EndVector(len(pingOffsets))

	sourceOff := builder.CreateString(source)
	pb.PingBatchStart(builder)
	pb.PingBatchAddSource(builder, sourceOff)
	pb.PingBatchAddPings(builder, pingsVec)
	root := pb.PingBatchEnd(builder)

	builder.Finish(root)
	return builder.FinishedBytes()
}
