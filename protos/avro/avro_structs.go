package avro

import (
	"container/list"
	"time"

	"github.com/elastic/libbeat/common"
)

// Avro Message
type AvroMessage struct {
	Ts           time.Time
	TcpTuple     common.TcpTuple
	CmdlineTuple *common.CmdlineTuple

	Method    string
	Direction uint8
	IsRequest bool
	Size      uint64

	// list of the parsed avro records
	Fields *list.List

	//Timing
	start int
	end   int
}

type AvroStream struct {
	tcptuple *common.TcpTuple

	data []byte

	message *AvroMessage
}

type AvroTransaction struct {
	Type         string
	tuple        common.TcpTuple
	Src          common.Endpoint
	Dst          common.Endpoint
	ResponseTime int32
	Ts           int64
	ts           time.Time
	cmdline      *common.CmdlineTuple
	BytesOut     uint64
	BytesIn      uint64
	Notes        []string

	Avro *list.List
}

type avroPrivateData struct {
	Data [2]*AvroStream
}
