package avro

import (
	"bytes"
	"container/list"
	"fmt"

	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/linkedin/goavro"
)

//Parses an encoded avro record into a list of Maps.
func parseAvro(input []byte) (*list.List, error) {

	fr, err := goavro.NewReader(goavro.FromReader(bytes.NewReader(input)))
	if err != nil {
		logp.Err("avro", "Unable to create reader for avro input: %s", input)
		return nil, err
	}

	records := list.New()

	for fr.Scan() {
		datum, err := fr.Read()
		if err != nil {
			logp.Err("avro", "Unable to read avro.")
			return nil, err
		}

		record, ok := datum.(*goavro.Record)
		if !ok {
			logp.Debug("avro", "Expected: *goavro.Record, actual: %T; ", datum)
		}

		// TODO maybe also return record.Name?
		records.PushBack(avroRecordToMap(record))
	}

	logp.Debug("avrodetailed", "Returning %d avro records", records.Len())

	return records, nil
}

func avroRecordToMap(record *goavro.Record) common.MapStr {
	avroMap := common.MapStr{}

	for _, field := range record.Fields {
		avroMap[field.Name] = fmt.Sprintf("%v", field.Datum)
	}
	logp.Debug("avrodetailed", "Parsed avro: %s", avroMap)
	return avroMap
}
