package avro

import (
	"encoding/hex"
	"net"
	"strings"
	"testing"

	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"
	"github.com/elastic/packetbeat/protos"
	"github.com/stretchr/testify/assert"
)

// Helper function returning a Avro module that can be used
// in tests. It publishes the transactions in the results channel.
func AvroModForTests() *Avro {
	var avro Avro
	results := publisher.ChanClient{make(chan common.MapStr, 10)}
	avro.Init(true, results)
	return &avro
}

// Helper function that returns an example TcpTuple
func testTcpTuple() *common.TcpTuple {
	t := &common.TcpTuple{
		Ip_length: 4,
		Src_ip:    net.IPv4(192, 168, 0, 1), Dst_ip: net.IPv4(192, 168, 0, 2),
		Src_port: 6512, Dst_port: 27017,
	}
	t.ComputeHashebles()
	return t
}

// Helper function to read from the results Queue. Raises
// an error if nothing is found in the queue.
func expectTransaction(t *testing.T, avro *Avro) common.MapStr {
	client := avro.results.(publisher.ChanClient)
	select {
	case trans := <-client.Channel:
		return trans
	default:
		t.Error("No transaction")
	}
	return nil
}

// Test simple request / response.
func TestSimpleFindLimit1(t *testing.T) {
	if testing.Verbose() {
		logp.LogInit(logp.LOG_DEBUG, "", false, true, []string{"avro", "avrodetailed"})
	}

	avro := AvroModForTests()

	requestHex := "7b22 646f 633a 223a 2241 2062" +
		"6173 6963 2073 6368 656d 6120 666f 7220" +
		"7374 6f72 696e 6720 626c 6f67 2063 6f6d" +
		"6d65 6e74 7322 2c22 6669 656c 6473 223a" +
		"5b7b 2264 6f63 223a 224e 616d 6520 6f66" +
		"2075 7365 7222 2c22 6e61 6d65 223a 2275" +
		"7365 726e 616d 6522 2c22 7479 7065 223a" +
		"2273 7472 696e 6722 7d2c 7b22 646f 6322" +
		"3a22 5468 6520 636f 6e74 656e 7420 6f66" +
		"2074 6865 2075 7365 7227 7320 6d65 7373" +
		"6167 6522 2c22 6e61 6d65 223a 2263 6f6d" +
		"6d65 6e74 222c 2274 7970 6522 3a22 7374" +
		"7269 6e67 227d 5d2c 226e 616d 6522 3a22" +
		"636f 6d6d 656e 7473 222c 226e 616d 6573" +
		"7061 6365 223a 2263 6f6d 2e65 7861 6d70" +
		"6c65 222c 2274 7970 6522 3a22 7265 636f" +
		"7264 227d"

	req_data, err := hex.DecodeString(strings.Replace(requestHex, " ", "", -1))
	assert.Nil(t, err)

	logp.Warn("Avro", req_data)

	resp_data, err := hex.DecodeString("")
	assert.Nil(t, err)

	tcptuple := testTcpTuple()
	req := protos.Packet{Payload: req_data}
	resp := protos.Packet{Payload: resp_data}

	private := protos.ProtocolData(new(avroPrivateData))

	private = avro.Parse(&req, tcptuple, 0, private)
	private = avro.Parse(&resp, tcptuple, 1, private)

	trans := expectTransaction(t, avro)

	//assert.Equal(t, "OK", trans["status"])
	//assert.Equal(t, "find", trans["method"])
	//assert.Equal(t, "avrp", trans["type"])

	logp.Debug("Avro", "Trans: %v", trans)
}
