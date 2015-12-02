# Avro protocol parsing for packetbeat

Main documentation links:

  - [Avro spec](https://avro.apache.org/docs/1.7.7/spec.html)
  - [Golang Avro library](https://github.com/linkedin/goavro)

## Communication protocols

The Avro spec doesn't specify a particular communication protocol for transport. 
The popular approaches seem to be asynchronously using just TCP, or synchronously using HTTP.
With the HTTP approach the avro data is posted as binary data, with content type of 'avro/binary'.

This implementation however is currently for asynchronous transport over TCP only.


## TODO

  - Add more system tests
  - Test with a more complex Avro schema


