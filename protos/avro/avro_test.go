package avro

import (
	"encoding/hex"
	"net"
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

// Test single packet.
func TestSinglePacket(t *testing.T) {
	if testing.Verbose() {
		logp.LogInit(logp.LOG_DEBUG, "", false, true, []string{"avro", "avrodetailed"})
	}

	avro := AvroModForTests()

	avroAsHex :=
		"4f626a0104166176726f2e736368656d61be047b2274797065223a2022726563" +
			"6f7264222c20226d65737361676573223a207b2273656e64223a207b22726571" +
			"75657374223a205b7b2274797065223a2022456d61696c222c20226e616d6522" +
			"3a2022656d61696c227d5d2c2022726573706f6e7365223a2022737472696e67" +
			"227d7d2c20226e616d65223a2022456d61696c222c20226669656c6473223a20" +
			"5b7b2274797065223a2022737472696e67222c20226e616d65223a2022746f22" +
			"7d2c207b2274797065223a2022737472696e67222c20226e616d65223a202266" +
			"726f6d227d2c207b2274797065223a2022737472696e67222c20226e616d6522" +
			"3a20227375626a656374227d2c207b2274797065223a2022737472696e67222c" +
			"20226e616d65223a2022626f6479227d5d7d146176726f2e636f646563086e75" +
			"6c6c001427186c9d0d21cbcf68ac8053b1441a02740e656c61737469630c6461" +
			"7272656e0a68656c6c6f48492062652074686520626f6479206f6620796f7572" +
			"20656e636f646564206176726f21211427186c9d0d21cbcf68ac8053b1441a"

	req_data, err := hex.DecodeString(avroAsHex)
	assert.Nil(t, err)

	tcptuple := testTcpTuple()
	req := protos.Packet{Payload: req_data}

	private := protos.ProtocolData(new(avroPrivateData))

	private = avro.Parse(&req, tcptuple, 0, private)

	trans := expectTransaction(t, avro)

	assert.Equal(t, "OK", trans["status"])
	assert.Equal(t, "avro", trans["type"])
}

// Test split packet.
func TestSplitPackets(t *testing.T) {
	if testing.Verbose() {
		logp.LogInit(logp.LOG_DEBUG, "", false, true, []string{"avro", "avrodetailed"})
	}

	avro := AvroModForTests()

	avroAsHex :=
		"4f626a0104166176726f2e736368656d61be047b2274797065223a2022726563" +
			"6f7264222c20226d65737361676573223a207b2273656e64223a207b22726571" +
			"75657374223a205b7b2274797065223a2022456d61696c222c20226e616d6522" +
			"3a2022656d61696c227d5d2c2022726573706f6e7365223a2022737472696e67" +
			"227d7d2c20226e616d65223a2022456d61696c222c20226669656c6473223a20" +
			"5b7b2274797065223a2022737472696e67222c20226e616d65223a2022746f22" +
			"7d2c207b2274797065223a2022737472696e67222c20226e616d65223a202266" +
			"726f6d227d2c207b2274797065223a2022737472696e67222c20226e616d6522" +
			"3a20227375626a656374227d2c207b2274797065223a2022737472696e67222c" +
			"20226e616d65223a2022626f6479227d5d7d146176726f2e636f646563086e75" +
			"6c6c001427186c9d0d21cbcf68ac8053b1441a02740e656c61737469630c6461" +
			"7272656e0a68656c6c6f48492062652074686520626f6479206f6620796f7572" +
			"20656e636f646564206176726f21211427186c9d0d21cbcf68ac8053b1441a"

	// need +1 offsets to ensure correct length hex strings
	req_data_1, err := hex.DecodeString(avroAsHex[:1+len(avroAsHex)/2])
	assert.Nil(t, err)

	req_data_2, err := hex.DecodeString(avroAsHex[len(avroAsHex)/2+1:])
	assert.Nil(t, err)

	tcptuple := testTcpTuple()

	req := protos.Packet{Payload: req_data_1}
	private := protos.ProtocolData(new(avroPrivateData))

	private = avro.Parse(&req, tcptuple, 0, private)

	req = protos.Packet{Payload: req_data_2}

	private = avro.Parse(&req, tcptuple, 0, private)

	trans := expectTransaction(t, avro)

	assert.Equal(t, "OK", trans["status"])
	assert.Equal(t, "avro", trans["type"])
}
