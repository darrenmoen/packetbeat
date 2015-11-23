package avro

import (
	"time"

	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"
	"github.com/elastic/packetbeat/config"
	"github.com/elastic/packetbeat/procs"
	"github.com/elastic/packetbeat/protos"
	"github.com/elastic/packetbeat/protos/tcp"
)

type Avro struct {
	// config
	Ports         []int
	Send_request  bool
	Send_response bool

	transactions       *common.Cache
	transactionTimeout time.Duration

	results publisher.Client
}

var debug = logp.MakeDebug("avro")

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

func (avro *Avro) SetFromConfig(config config.Avro) (err error) {

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
	debug("init avro plugin")
	avro.InitDefaults()

	if !test_mode {
		err := avro.SetFromConfig(config.ConfigSingleton.Protocols.Avro)
		if err != nil {
			return err
		}
	}

	avro.transactions = common.NewCache(
		avro.transactionTimeout,
		protos.DefaultTransactionHashSize)
	avro.transactions.StartJanitor(avro.transactionTimeout)
	avro.results = results

	return nil
}

func (avro *Avro) messageParser(s *AvroStream) (bool, bool) {

	//var cont, ok, complete bool
	m := s.message
	debug("avro", "messageParser")
	// TODO parse message
	trans := avro.getTransaction(m.TcpTuple.Hashable())
	if trans != nil {
		m.IsRequest = false
	} else {
		m.IsRequest = true
	}
	
	//codec, err := goavro.NewCodec(someRecordSchemaJson)
	
	return true, true
}

// messageGap is called when a gap of size `nbytes` is found in the
// tcp stream. Decides if we can ignore the gap or it's a parser error
// and we need to drop the stream.
func (avro *Avro) messageGap(s *AvroStream, nbytes int) (ok bool, complete bool) {

	logp.Debug("avro", "messageGap")

	// assume we cannot recover
	return false, false
}

func (stream *AvroStream) PrepareForNewMessage() {
	logp.Debug("avro", "PrepareForNewMessage")
	stream.data = stream.data[stream.message.end:]
	stream.parseOffset = 0
	stream.bodyReceived = 0
	stream.message = nil
}

type avroPrivateData struct {
	Data [2]*AvroStream
}

// Called when the parser has identified the boundary
// of a message.
func (avro *Avro) messageComplete(tcptuple *common.TcpTuple, dir uint8, stream *AvroStream) {
	logp.Debug("avro", "messageComplete")
	//msg := stream.data[stream.message.start:stream.message.end]
	msg := stream.data

	avro.handleAvro(stream.message, tcptuple, dir, msg)

	// and reset message
	stream.PrepareForNewMessage()
}

func (avro *Avro) ConnectionTimeout() time.Duration {
	return avro.transactionTimeout
}

func getPrivateData(private protos.ProtocolData) *avroPrivateData {
	if private == nil {
		return &avroPrivateData{}
	}

	priv, ok := private.(*avroPrivateData)
	if !ok {
		logp.Warn("avro connection data type error, create new one")
		return &avroPrivateData{}
	}
	if priv == nil {
		logp.Warn("Unexpected: avro private data not set, create new one")
		return &avroPrivateData{}
	}
	return priv
}

func (avro *Avro) Parse(pkt *protos.Packet, tcptuple *common.TcpTuple,
	dir uint8, private protos.ProtocolData) protos.ProtocolData {

	defer logp.Recover("ParseHttp exception")

	priv := getPrivateData(private)

	logp.Debug("avrodetailed", "Parse payload received: [%s]", dir)

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
	if stream.message == nil {
		stream.message = &AvroMessage{Ts: pkt.Ts}
	}
	ok, complete := avro.messageParser(stream)

	if !ok {
		// drop this tcp stream. Will retry parsing with the next
		// segment in it
		priv.Data[dir] = nil
		return priv
	}

	if complete {
		// all ok, ship it
		avro.messageComplete(tcptuple, dir, stream)
	}

	return priv
}

func (avro *Avro) ReceivedFin(tcptuple *common.TcpTuple, dir uint8,
	private protos.ProtocolData) protos.ProtocolData {

	if private == nil {
		return private
	}
	avroData, ok := private.(avroPrivateData)
	if !ok {
		return private
	}
	if avroData.Data[dir] == nil {
		return avroData
	}

	stream := avroData.Data[dir]

	// send whatever data we got so far as complete. This
	// is needed for the HTTP/1.0 without Content-Length situation.
	if stream.message != nil &&
		len(stream.data[stream.message.start:]) > 0 {

		logp.Debug("avrodetailed", "Publish something on connection FIN")

		msg := stream.data[stream.message.start:]

		avro.handleAvro(stream.message, tcptuple, dir, msg)

		// and reset message. Probably not needed, just to be sure.
		stream.PrepareForNewMessage()
	}

	return avroData
}

// Called when a gap of nbytes bytes is found in the stream (due to
// packet loss).
func (avro *Avro) GapInStream(tcptuple *common.TcpTuple, dir uint8,
	nbytes int, private protos.ProtocolData) (priv protos.ProtocolData, drop bool) {
	logp.Debug("avro", "GapInStream")
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

	ok, complete := avro.messageGap(stream, nbytes)
	logp.Debug("avrodetailed", "messageGap returned ok=%v complete=%v", ok, complete)
	if !ok {
		// on errors, drop stream
		avroData.Data[dir] = nil
		return avroData, true
	}

	if complete {
		// Current message is complete, we need to publish from here
		avro.messageComplete(tcptuple, dir, stream)
	}

	// don't drop the stream, we can ignore the gap
	return private, false
}

func (avro *Avro) handleAvro(m *AvroMessage, tcptuple *common.TcpTuple,
	dir uint8, raw_msg []byte) {
	m.TcpTuple = *tcptuple
	m.Direction = dir
	m.CmdlineTuple = procs.ProcWatcher.FindProcessesTuple(tcptuple.IpPort())

	if m.IsRequest {
		avro.receivedAvroRequest(m)
	} else {
		avro.receivedAvroResponse(m)
	}
}

func (avro *Avro) receivedAvroRequest(msg *AvroMessage) {
	logp.Debug("avro", "receivedAvroRequest")
	trans := avro.getTransaction(msg.TcpTuple.Hashable())
	if trans != nil {
		if len(trans.Avro) != 0 {
			logp.Warn("Two requests without a response. Dropping old request")
		}
	} else {
		trans = &AvroTransaction{Type: "avro", tuple: msg.TcpTuple}
		avro.transactions.Put(msg.TcpTuple.Hashable(), trans)
	}

	logp.Debug("avro", "Received request with tuple: %s", msg.TcpTuple)

	trans.ts = msg.Ts
	trans.Ts = int64(trans.ts.UnixNano() / 1000)
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

	//trans.Request_raw = string(avro.cutMessageBody(msg))

	trans.BytesIn = msg.Size
	trans.Notes = msg.Notes

	trans.Avro = common.MapStr{}
	trans.Real_ip = msg.Real_ip

	var err error
	//trans.Path, trans.Params, err = avro.extractParameters(msg, msg.Raw)
	if err != nil {
		logp.Warn("avro", "Fail to parse HTTP parameters: %v", err)
	}
}

func (avro *Avro) receivedAvroResponse(msg *AvroMessage) {
	logp.Debug("avro", "receivedAvroResponse")
	// we need to search the request first.
	tuple := msg.TcpTuple

	logp.Debug("avro", "Received response with tuple: %s", tuple)

	trans := avro.getTransaction(tuple.Hashable())
	if trans == nil {
		logp.Warn("Response from unknown transaction. Ignoring: %v", tuple)
		return
	}

	if trans.Avro == nil {
		logp.Warn("Response without a known request. Ignoring.")
		return
	}

	response := common.MapStr{
		"code":           "ddd",
		"content_length": "ddd2",
	}

	trans.BytesOut = msg.Size
	trans.Avro.Update(response)
	trans.Notes = append(trans.Notes, msg.Notes...)

	trans.ResponseTime = int32(msg.Ts.Sub(trans.ts).Nanoseconds() / 1e6) // resp_time in milliseconds

	avro.publishTransaction(trans)
	avro.transactions.Delete(trans.tuple.Hashable())

	logp.Debug("avro", "HTTP transaction completed: %s\n", trans.Avro)
}

func (avro *Avro) publishTransaction(t *AvroTransaction) {
	logp.Debug("avro", "publishTransaction")
	if avro.results == nil {
		return
	}

	event := common.MapStr{}

	event["type"] = "avro"
	code := t.Avro["code"].(uint16)
	if code < 400 {
		event["status"] = common.OK_STATUS
	} else {
		event["status"] = common.ERROR_STATUS
	}
	event["responsetime"] = t.ResponseTime

	event["avro"] = t.Avro
	if len(t.Real_ip) > 0 {
		event["real_ip"] = t.Real_ip
	}

	event["bytes_out"] = t.BytesOut
	event["bytes_in"] = t.BytesIn
	event["@timestamp"] = common.Time(t.ts)
	event["src"] = &t.Src
	event["dst"] = &t.Dst

	if len(t.Notes) > 0 {
		event["notes"] = t.Notes
	}

	avro.results.PublishEvent(event)
}

func (avro *Avro) cutMessageBody(m *AvroMessage) []byte {
	raw_msg_cut := []byte{}

	// add headers always
	raw_msg_cut = m.Raw[:m.bodyOffset]

	// add body
	/*
		if len(m.ContentType) == 0 || avro.shouldIncludeInBody(m.ContentType) {
			if len(m.chunked_body) > 0 {
				raw_msg_cut = append(raw_msg_cut, m.chunked_body...)
			} else {
				logp.Debug("avro", "Body to include: [%s]", m.Raw[m.bodyOffset:])
				raw_msg_cut = append(raw_msg_cut, m.Raw[m.bodyOffset:]...)
			}
		}
	*/

	return raw_msg_cut
}

func (avro *Avro) shouldIncludeInBody(contenttype string) bool {
	return true
}
