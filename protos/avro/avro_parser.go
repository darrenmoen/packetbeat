package avro 

import (
	"bytes"
	"fmt"
	
	"github.com/elastic/libbeat/common"
	"github.com/linkedin/goavro"
	"github.com/elastic/libbeat/logp"
)

//Parses an encoded avro record into a Map.
func parseAvro(input []byte) (common.MapStr, error) {
	
	logp.Debug("avro", "parsing Avro")

	fr, err := goavro.NewReader(goavro.FromReader(bytes.NewReader(input)))
	if err != nil {
		logp.Err("avro", "Unable to create reader for avro input.")
	}
	
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
		// TODO maybe return record.Name?
		return avroRecordToMap(record), nil
	}
	
	return nil, err
}

func avroRecordToMap(record *goavro.Record) (common.MapStr) {
	avroMap := common.MapStr{}
	
	for _, field := range record.Fields {
		avroMap[field.Name] = fmt.Sprintf("%v", field.Datum)
	}
	logp.Debug("avro", "Parsed avro: %s", avroMap)
	return avroMap
}
