package avro

import (
	"time"

	"github.com/elastic/libbeat/common"
)

// Avro Message
type AvroMessage struct {
	Ts             time.Time
	TcpTuple       common.TcpTuple
	CmdlineTuple   *common.CmdlineTuple
	Direction      uint8
	IsRequest      bool
	Size           uint64
	Notes          []string
	Real_ip        string
	Raw            []byte
	chunked_length int
	chunked_body   []byte
	bodyOffset     int

	//Timing
	start int
	end   int
}

type AvroStream struct {
	tcptuple *common.TcpTuple

	data []byte

	parseOffset  int
	parseState   int
	bodyReceived int

	message *AvroMessage
}

type AvroTransaction struct {
	Type         string
	tuple        common.TcpTuple
	Src          common.Endpoint
	Dst          common.Endpoint
	Real_ip      string
	ResponseTime int32
	Ts           int64
	ts           time.Time
	cmdline      *common.CmdlineTuple
	BytesOut     uint64
	BytesIn      uint64
	Notes        []string

	Avro common.MapStr
}
