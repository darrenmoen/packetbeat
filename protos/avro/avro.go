package avro

import (
	"errors"
	"fmt"
	"time"

	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"

	"github.com/elastic/packetbeat/config"
	"github.com/elastic/packetbeat/procs"
	"github.com/elastic/packetbeat/protos"
	"github.com/elastic/packetbeat/protos/tcp"
)

// Packet types
const (
	MYSQL_CMD_QUERY = 3
)

const MAX_PAYLOAD_SIZE = 100 * 1024

type AvroMessage struct {
	start int
	end   int

	Ts            time.Time
	IsRequest     bool
	PacketLength  uint32
	Seq           uint8
	Typ           uint8
	IgnoreMessage bool
	Direction     uint8
	IsTruncated   bool
	TcpTuple      common.TcpTuple
	CmdlineTuple  *common.CmdlineTuple
	Raw           []byte
	Notes         []string
}

type AvroTransaction struct {
	Type         string
	tuple        common.TcpTuple
	Src          common.Endpoint
	Dst          common.Endpoint
	ResponseTime int32
	Ts           int64
	JsTs         time.Time
	ts           time.Time
	Query        string
	Method       string
	Path         string // for mysql, Path refers to the mysql table queried
	BytesOut     uint64
	BytesIn      uint64
	Notes        []string

	Avro common.MapStr

	Request_raw  string
	Response_raw string
}

type AvroStream struct {
	tcptuple *common.TcpTuple

	data []byte

	parseOffset int
	parseState  parseState
	isClient    bool

	message *AvroMessage
}

type parseState int

const (
	avroStateStart parseState = iota
	avroStateEatMessage
	avroStateEatFields
	avroStateEatRows

	AvroStateMax
)

var stateStrings []string = []string{
	"Start",
	"EatMessage",
	"EatFields",
	"EatRows",
}

func (state parseState) String() string {
	return stateStrings[state]
}

type Avro struct {

	// config
	Ports         []int
	Send_request  bool
	Send_response bool

	transactions       *common.Cache
	transactionTimeout time.Duration

	results publisher.Client

	// function pointer for mocking
	handleAvro func(mysql *Avro, m *AvroMessage, tcp *common.TcpTuple,
		dir uint8, raw_msg []byte)
}

func (avro *Avro) getTransaction(k common.HashableTcpTuple) *AvroTransaction {
	v := avro.transactions.Get(k)
	if v != nil {
		return v.(*AvroTransaction)
	}
	return nil
}

func (avro *Avro) InitDefaults() {
	avro.Send_request = false
	avro.Send_response = false
	avro.transactionTimeout = protos.DefaultTransactionExpiration
}

func (avro *Avro) setFromConfig(config config.Avro) error {

	avro.Ports = config.Ports

	if config.SendRequest != nil {
		avro.Send_request = *config.SendRequest
	}
	if config.SendResponse != nil {
		avro.Send_response = *config.SendResponse
	}
	if config.TransactionTimeout != nil && *config.TransactionTimeout > 0 {
		avro.transactionTimeout = time.Duration(*config.TransactionTimeout) * time.Second
	}
	return nil
}

func (avro *Avro) GetPorts() []int {
	return avro.Ports
}

func (avro *Avro) Init(test_mode bool, results publisher.Client) error {

	avro.InitDefaults()
	if !test_mode {
		err := avro.setFromConfig(config.ConfigSingleton.Protocols.Avro)
		if err != nil {
			return err
		}
	}

	avro.transactions = common.NewCache(
		avro.transactionTimeout,
		protos.DefaultTransactionHashSize)
	avro.transactions.StartJanitor(avro.transactionTimeout)
	avro.handleAvro = handleAvro
	avro.results = results

	return nil
}

func (stream *AvroStream) PrepareForNewMessage() {
	stream.data = stream.data[stream.parseOffset:]
	stream.parseState = avroStateStart
	stream.parseOffset = 0
	stream.isClient = false
	stream.message = nil
}

func avroMessageParser(s *AvroStream) (bool, bool) {

	logp.Debug("avrodetailed", "Avro parser called. parseState = %s", s.parseState)

	//m := s.message

	// TODO ...

	return true, false
}

// messageGap is called when a gap of size `nbytes` is found in the
// tcp stream. Returns true if there is already enough data in the message
// read so far that we can use it further in the stack.
func (avro *Avro) messageGap(s *AvroStream, nbytes int) (complete bool) {

	m := s.message
	switch s.parseState {
	case avroStateStart, avroStateEatMessage:
		// not enough data yet to be useful
		return false
	case avroStateEatFields, avroStateEatRows:
		// enough data here
		m.end = s.parseOffset
		if m.IsRequest {
			m.Notes = append(m.Notes, "Packet loss while capturing the request")
		} else {
			m.Notes = append(m.Notes, "Packet loss while capturing the response")
		}
		return true
	}

	return true
}

type avroPrivateData struct {
	Data [2]*AvroStream
}

// Called when the parser has identified a full message.
func (avro *Avro) messageComplete(tcptuple *common.TcpTuple, dir uint8, stream *AvroStream) {
	// all ok, ship it
	msg := stream.data[stream.message.start:stream.message.end]

	if !stream.message.IgnoreMessage {
		avro.handleAvro(avro, stream.message, tcptuple, dir, msg)
	}

	// and reset message
	stream.PrepareForNewMessage()
}

func (avro *Avro) ConnectionTimeout() time.Duration {
	return avro.transactionTimeout
}

func (avro *Avro) Parse(pkt *protos.Packet, tcptuple *common.TcpTuple,
	dir uint8, private protos.ProtocolData) protos.ProtocolData {

	defer logp.Recover("AvroMysql exception")

	priv := avroPrivateData{}
	if private != nil {
		var ok bool
		priv, ok = private.(avroPrivateData)
		if !ok {
			priv = avroPrivateData{}
		}
	}

	if priv.Data[dir] == nil {
		priv.Data[dir] = &AvroStream{
			tcptuple: tcptuple,
			data:     pkt.Payload,
			message:  &AvroMessage{Ts: pkt.Ts},
		}
	} else {
		// concatenate bytes
		priv.Data[dir].data = append(priv.Data[dir].data, pkt.Payload...)
		if len(priv.Data[dir].data) > tcp.TCP_MAX_DATA_IN_STREAM {
			logp.Debug("avro", "Stream data too large, dropping TCP stream")
			priv.Data[dir] = nil
			return priv
		}
	}

	stream := priv.Data[dir]
	for len(stream.data) > 0 {
		if stream.message == nil {
			stream.message = &AvroMessage{Ts: pkt.Ts}
		}

		ok, complete := avroMessageParser(priv.Data[dir])
		//logp.Debug("avrodetailed", "avroMessageParser returned ok=%b complete=%b", ok, complete)
		if !ok {
			// drop this tcp stream. Will retry parsing with the next
			// segment in it
			priv.Data[dir] = nil
			logp.Debug("avro", "Ignore Avro message. Drop tcp stream. Try parsing with the next segment")
			return priv
		}

		if complete {
			avro.messageComplete(tcptuple, dir, stream)
		} else {
			// wait for more data
			break
		}
	}
	return priv
}

func (avro *Avro) GapInStream(tcptuple *common.TcpTuple, dir uint8,
	nbytes int, private protos.ProtocolData) (priv protos.ProtocolData, drop bool) {

	defer logp.Recover("GapInStream(avro) exception")

	if private == nil {
		return private, false
	}
	avroData, ok := private.(avroPrivateData)
	if !ok {
		return private, false
	}
	stream := avroData.Data[dir]
	if stream == nil || stream.message == nil {
		// nothing to do
		return private, false
	}

	if avro.messageGap(stream, nbytes) {
		// we need to publish from here
		avro.messageComplete(tcptuple, dir, stream)
	}

	// we always drop the TCP stream. Because it's binary and len based,
	// there are too few cases in which we could recover the stream (maybe
	// for very large blobs, leaving that as TODO)
	return private, true
}

func (avro *Avro) ReceivedFin(tcptuple *common.TcpTuple, dir uint8,
	private protos.ProtocolData) protos.ProtocolData {

	// TODO: check if we have data pending and either drop it to free
	// memory or send it up the stack.
	return private
}

func handleAvro(avro *Avro, m *AvroMessage, tcptuple *common.TcpTuple,
	dir uint8, raw_msg []byte) {

	m.TcpTuple = *tcptuple
	m.Direction = dir
	m.CmdlineTuple = procs.ProcWatcher.FindProcessesTuple(tcptuple.IpPort())
	m.Raw = raw_msg

	if m.IsRequest {
		avro.receivedAvroRequest(m)
	} else {
		avro.receivedAvroResponse(m)
	}
}

func (avro *Avro) receivedAvroRequest(msg *AvroMessage) {
	tuple := msg.TcpTuple
	trans := avro.getTransaction(tuple.Hashable())
	if trans != nil {
		if trans.Avro != nil {
			logp.Debug("avro", "Two requests without a Response. Dropping old request: %s", trans.Avro)
		}
	} else {
		trans = &AvroTransaction{Type: "avro", tuple: tuple}
		avro.transactions.Put(tuple.Hashable(), trans)
	}

	trans.ts = msg.Ts
	trans.Ts = int64(trans.ts.UnixNano() / 1000) // transactions have microseconds resolution
	trans.JsTs = msg.Ts
	trans.Src = common.Endpoint{
		Ip:   msg.TcpTuple.Src_ip.String(),
		Port: msg.TcpTuple.Src_port,
		Proc: string(msg.CmdlineTuple.Src),
	}
	trans.Dst = common.Endpoint{
		Ip:   msg.TcpTuple.Dst_ip.String(),
		Port: msg.TcpTuple.Dst_port,
		Proc: string(msg.CmdlineTuple.Dst),
	}
	if msg.Direction == tcp.TcpDirectionReverse {
		trans.Src, trans.Dst = trans.Dst, trans.Src
	}

	// Extract the method, by simply taking the first word and
	// making it upper case.
	/*query := strings.Trim(msg.Query, " \n\t")
	index := strings.IndexAny(query, " \n\t")
	var method string
	if index > 0 {
		method = strings.ToUpper(query[:index])
	} else {
		method = strings.ToUpper(query)
	}

	trans.Query = query
	trans.Method = method

	trans.Avro = common.MapStr{}

	trans.Notes = msg.Notes

	// save Raw message
	trans.Request_raw = msg.Query
	trans.BytesIn = msg.Size
	*/
}

func (avro *Avro) receivedAvroResponse(msg *AvroMessage) {
	trans := avro.getTransaction(msg.TcpTuple.Hashable())
	if trans == nil {
		logp.Warn("Response from unknown transaction. Ignoring.")
		return
	}
	// check if the request was received
	if trans.Avro == nil {
		logp.Warn("Response from unknown transaction. Ignoring.")
		return

	}
	// save json details
	/*
		trans.Mysql.Update(common.MapStr{
			"affected_rows": msg.AffectedRows,
			"insert_id":     msg.InsertId,
			"num_rows":      msg.NumberOfRows,
			"num_fields":    msg.NumberOfFields,
			"iserror":       msg.IsError,
			"error_code":    msg.ErrorCode,
			"error_message": msg.ErrorInfo,
		})
		trans.BytesOut = msg.Size
		trans.Path = msg.Tables

		trans.ResponseTime = int32(msg.Ts.Sub(trans.ts).Nanoseconds() / 1e6) // resp_time in milliseconds

		// save Raw message
		if len(msg.Raw) > 0 {
			fields, rows := mysql.parseMysqlResponse(msg.Raw)

			trans.Response_raw = common.DumpInCSVFormat(fields, rows)
		}

		trans.Notes = append(trans.Notes, msg.Notes...)

		mysql.publishTransaction(trans)
		mysql.transactions.Delete(trans.tuple.Hashable())
	*/
	logp.Debug("avro", "Avro transaction completed: %s", trans.Avro)
	logp.Debug("avro", "%s", trans.Response_raw)
}

func (avro *Avro) parseAvroResponse(data []byte) ([]string, [][]string) {

	length, err := read_length(data, 0)
	if err != nil {
		logp.Warn("Invalid response: %v", err)
		return []string{}, [][]string{}
	}
	if length < 1 {
		logp.Warn("Warning: Skipping empty Response")
		return []string{}, [][]string{}
	}

	fields := []string{}
	rows := [][]string{}

	if len(data) < 5 {
		logp.Warn("Invalid response: data less than 4 bytes")
		return []string{}, [][]string{}
	}

	if uint8(data[4]) == 0x00 {
		// OK response
	} else if uint8(data[4]) == 0xff {
		// Error response
	}

	// TODO ...

	return fields, rows
}

func (avro *Avro) publishTransaction(t *AvroTransaction) {

	if avro.results == nil {
		return
	}

	logp.Debug("avro", "avro.results exists")

	event := common.MapStr{}
	event["type"] = "avro"

	if t.Avro["iserror"].(bool) {
		event["status"] = common.ERROR_STATUS
	} else {
		event["status"] = common.OK_STATUS
	}

	event["responsetime"] = t.ResponseTime
	if avro.Send_request {
		event["request"] = t.Request_raw
	}
	if avro.Send_response {
		event["response"] = t.Response_raw
	}
	//event["method"] = t.Method
	//event["query"] = t.Query
	//event["mysql"] = t.Mysql
	//event["path"] = t.Path
	event["bytes_out"] = t.BytesOut
	event["bytes_in"] = t.BytesIn

	if len(t.Notes) > 0 {
		event["notes"] = t.Notes
	}

	event["@timestamp"] = common.Time(t.ts)
	event["src"] = &t.Src
	event["dst"] = &t.Dst

	avro.results.PublishEvent(event)
}

func read_lstring(data []byte, offset int) ([]byte, int, bool, error) {
	length, off, complete, err := read_linteger(data, offset)
	if err != nil {
		return nil, 0, false, err
	}
	if !complete || len(data[off:]) < int(length) {
		return nil, 0, false, nil
	}

	return data[off : off+int(length)], off + int(length), true, nil
}
func read_linteger(data []byte, offset int) (uint64, int, bool, error) {
	if len(data) < offset+1 {
		return 0, 0, false, nil
	}
	switch uint8(data[offset]) {
	case 0xfe:
		if len(data[offset+1:]) < 8 {
			return 0, 0, false, nil
		}
		return uint64(data[offset+1]) | uint64(data[offset+2])<<8 |
				uint64(data[offset+2])<<16 | uint64(data[offset+3])<<24 |
				uint64(data[offset+4])<<32 | uint64(data[offset+5])<<40 |
				uint64(data[offset+6])<<48 | uint64(data[offset+7])<<56,
			offset + 9, true, nil
	case 0xfd:
		if len(data[offset+1:]) < 3 {
			return 0, 0, false, nil
		}
		return uint64(data[offset+1]) | uint64(data[offset+2])<<8 |
			uint64(data[offset+3])<<16, offset + 4, true, nil
	case 0xfc:
		if len(data[offset+1:]) < 2 {
			return 0, 0, false, nil
		}
		return uint64(data[offset+1]) | uint64(data[offset+2])<<8, offset + 3, true, nil
	}

	if uint64(data[offset]) >= 0xfb {
		return 0, 0, false, fmt.Errorf("Unexpected value in read_linteger")
	}

	return uint64(data[offset]), offset + 1, true, nil
}

// Read a avro length field (3 bytes LE)
func read_length(data []byte, offset int) (int, error) {
	if len(data[offset:]) < 3 {
		return 0, errors.New("Data too small to contain a valid length")
	}
	length := uint32(data[offset]) |
		uint32(data[offset+1])<<8 |
		uint32(data[offset+2])<<16
	return int(length), nil
}
