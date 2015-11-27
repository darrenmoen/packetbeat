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
	Ports        []int
	Asynchronous bool

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
	avro.Asynchronous = true
	avro.transactionTimeout = protos.DefaultTransactionExpiration
}

func (avro *Avro) SetFromConfig(config config.Avro) (err error) {

	avro.Ports = config.Ports

	if config.Asynchronous != nil {
		avro.Asynchronous = *config.Asynchronous
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
	avroMap, err := parseAvro(s.data)
	if err == nil {
		logp.Debug("avro", "messageParser success")
		m := s.message
		m.Fields = avroMap
		return true, true
	}

	return true, false
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
	stream.data = []byte{}
	stream.message = nil
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

	logp.Debug("avrodetailed", "Parse payload received: [%v]", dir)
	logp.Debug("avrodetailed", "stream id: [%v]", tcptuple.Stream_id)

	if priv.Data[dir] == nil {
		priv.Data[dir] = &AvroStream{
			tcptuple: tcptuple,
			data:     pkt.Payload,
			message:  &AvroMessage{Ts: pkt.Ts, IsRequest: true},
		}
	} else {
		// append current packet to previous packets
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
		} else {
			// wait for more data
			break
		}
	}

	return priv
}

// Called when the parser has identified the boundary
// of a message.
func (avro *Avro) messageComplete(tcptuple *common.TcpTuple, dir uint8, stream *AvroStream) {
	logp.Debug("avro", "messageComplete")
	//msg := stream.data[stream.message.start:stream.message.end]
	stream.message.TcpTuple = *tcptuple
	stream.message.Direction = dir
	stream.message.Size = uint64(len(stream.data))
	stream.message.CmdlineTuple = procs.ProcWatcher.FindProcessesTuple(tcptuple.IpPort())

	avro.handleAvro(stream.message)

	// and reset message
	stream.PrepareForNewMessage()
}

func (avro *Avro) ReceivedFin(tcptuple *common.TcpTuple, dir uint8,
	private protos.ProtocolData) protos.ProtocolData {

	// TODO when I implement responses
	return private
}

// Called when a gap of nbytes bytes is found in the stream (due to
// packet loss).
func (avro *Avro) GapInStream(tcptuple *common.TcpTuple, dir uint8,
	nbytes int, private protos.ProtocolData) (priv protos.ProtocolData, drop bool) {

	return private, true
}

func (avro *Avro) handleAvro(msg *AvroMessage) {

	if msg.IsRequest {
		avro.receivedAvroRequest(msg)
	} else {
		avro.receivedAvroResponse(msg)
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

	trans.BytesIn = msg.Size
	trans.Avro = msg.Fields

	if avro.Asynchronous {
		avro.publishTransaction(trans)
		avro.transactions.Delete(trans.tuple.Hashable())
	}
}

func (avro *Avro) receivedAvroResponse(msg *AvroMessage) {
	/*
		Only dealing with asynscronous communication at the moment.

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

		trans.BytesOut = msg.Size
		trans.Avro = msg.Fields

		trans.ResponseTime = int32(msg.Ts.Sub(trans.ts).Nanoseconds() / 1e6) // resp_time in milliseconds

		avro.publishTransaction(trans)
		avro.transactions.Delete(trans.tuple.Hashable())
	*/
}

func (avro *Avro) publishTransaction(t *AvroTransaction) {
	logp.Debug("avro", "publishing avro transaction")
	if avro.results == nil {
		return
	}

	event := common.MapStr{}

	event["type"] = "avro"
	event["status"] = common.OK_STATUS
	event["responsetime"] = t.ResponseTime
	event["avro"] = t.Avro
	event["bytes_out"] = t.BytesOut
	event["bytes_in"] = t.BytesIn
	event["@timestamp"] = common.Time(t.ts)
	event["src"] = &t.Src
	event["dst"] = &t.Dst

	logp.Debug("avrodetailed", "publishing %s", event)
	avro.results.PublishEvent(event)
}
