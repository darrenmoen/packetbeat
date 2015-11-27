package avro

import (
	"encoding/hex"
	"testing"

	"github.com/elastic/libbeat/logp"
	"github.com/stretchr/testify/assert"
)

// Test an empty byte array input.
func TestEmptyInput(t *testing.T) {
	if testing.Verbose() {
		logp.LogInit(logp.LOG_DEBUG, "", false, true, []string{"avro", "avrodetailed"})
	}
	
	emptyByteArray := []byte("")
	
	avroAsJson, err := parseAvro(emptyByteArray)
	assert.NotNil(t, err)
	assert.Nil(t, avroAsJson)
}

// Test an uncomprocessed binary avro record.
func TestAvroParsingWithNoCompression(t *testing.T) {
	if testing.Verbose() {
		logp.LogInit(logp.LOG_DEBUG, "", false, true, []string{"avro", "avrodetailed"})
	}

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

	avroAsByteArray, err := hex.DecodeString(avroAsHex)
	assert.Nil(t, err)

	avroAsJson, err := parseAvro(avroAsByteArray)
	assert.Nil(t, err)
	assert.NotEmpty(t, avroAsJson)

	assert.Equal(t, "elastic", avroAsJson["to"])
	assert.Equal(t, "hello", avroAsJson["subject"])
}

// Test a deflate compressed binary avro record.
func TestAvroParsingWithDeflateCompression(t *testing.T) {
	if testing.Verbose() {
		logp.LogInit(logp.LOG_DEBUG, "", false, true, []string{"avro", "avrodetailed"})
	}

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
			"20226e616d65223a2022626f6479227d5d7d146176726f2e636f6465630e6465" +
			"666c6174650038da3e2a2aedbe6e80f7af22d198bb2e027c05c1510a80201004" +
			"d0ff88ce30dea68ea1ee84c1e2c06a81b7efbd839ec77cea6e39827d6b74d779" +
			"a110b31145b6a01b4b6f80bdca68c85f28a51f56191438da3e2a2aedbe6e80f7" +
			"af22d198bb2e"

	avroAsByteArray, err := hex.DecodeString(avroAsHex)
	assert.Nil(t, err)

	avroAsJson, err := parseAvro(avroAsByteArray)
	assert.Nil(t, err)
	assert.NotEmpty(t, avroAsJson)

	assert.Equal(t, "elastic", avroAsJson["to"])
	assert.Equal(t, "hello", avroAsJson["subject"])
}
