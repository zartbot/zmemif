package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zartbot/zmemif"

	_ "net/http/pprof"
)

func packetprocessing(p *zmemif.Port) {
	p.Wg.Add(1)
	defer p.Wg.Done()
	pkt := make([]byte, 2048)
	rxq0, err := p.GetRxQueue(0)
	if err != nil {
		logrus.Fatal("Get RX-Queue failed.")
	}
	txq0, err := p.GetTxQueue(0)
	if err != nil {
		logrus.Fatal("Get TX-Queue failed.")
	}
	//Server simply echo result to client
	for {
		pktLen, err := rxq0.ReadPacket(pkt)
		if err != nil {
			logrus.Warn("recv error:", err)
			continue
		}
		if pktLen > 0 {
			txq0.WritePacket(pkt[:pktLen])
		}
	}
}

func Connected(p *zmemif.Port) error {
	fmt.Println("Connected: ", p.GetName())
	go packetprocessing(p)
	return nil
}

func main() {
	socketName := flag.String("socket", "", "control socket filename")
	flag.Parse()
	cfg := &zmemif.PortCfg{
		Id:       0,
		Name:     "memif0",
		IsServer: true,
		MemoryConfig: zmemif.MemoryConfig{
			NumQueuePairs: 1,
		},
		ConnectedFunc: Connected,
	}

	ctrlSock, err := zmemif.NewSocket("foo", *socketName)
	if err != nil {
		logrus.Fatal("create socket failed: %v", err)
	}
	defer ctrlSock.Delete()

	port, err := zmemif.NewPort(ctrlSock, cfg, nil)
	if err != nil {
		logrus.Fatal(err)
	}

	ctrlSock.StartPolling()

	for {
		select {

		case err := <-ctrlSock.ErrChan:
			if err != nil {
				logrus.Fatal(err)
			}
		default:
			fmt.Printf("%s", port)
			time.Sleep(20 * time.Second)
		}
	}

}
