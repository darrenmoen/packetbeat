from pbtests.packetbeat import TestCase


class Test(TestCase):

    def test_avro_with_no_compression(self):
        self.render_config_template(avro_ports=[45454])
        self.run_packetbeat(pcap="avro/avro_tcp_null.pcap",
                            debug_selectors=["tcp", "avro", "avrodetailed", "publish"])

        objs = self.read_output()
        
        for obj in objs:
            print obj
        
        assert len(objs) == 1
