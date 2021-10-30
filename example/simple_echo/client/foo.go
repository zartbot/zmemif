package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zartbot/zmemif"

	_ "net/http/pprof"
)

func sendpkt(p *zmemif.Port) {
	p.Wg.Add(1)
	defer p.Wg.Done()
	txq0, err := p.GetTxQueue(0)
	if err != nil {
		logrus.Fatal("Get TX-Queue failed.")
	}
	//Client simply send result and calculate the RTT
	sendpkt := make([]byte, 800)
	ts := uint64(time.Now().UnixNano())
	for {
		select {
		case <-p.QuitChan: // channel closed
			return
		default:
			ts = uint64(time.Now().UnixNano())
			binary.BigEndian.PutUint64(sendpkt, ts)
			txq0.WritePacket(sendpkt)
			time.Sleep(1 * time.Second)
		}
	}
}

func recvpkt(p *zmemif.Port) {
	p.Wg.Add(1)
	defer p.Wg.Done()
	pkt := make([]byte, 2048)

	rxq0, err := p.GetRxQueue(0)
	if err != nil {
		logrus.Fatal("Get RX-Queue failed.")
	}

	for {
		pktLen, _ := rxq0.ReadPacket(pkt)
		if pktLen > 0 {
			recvts := binary.BigEndian.Uint64(pkt[:])
			logrus.Info("RTT:", time.Now().Sub(time.Unix(0, int64(recvts))))
		}

	}
}

func Connected(p *zmemif.Port) error {
	fmt.Println("Connected: ", p.GetName())
	go sendpkt(p)
	go recvpkt(p)
	return nil
}

func main() {
	socketName := flag.String("socket", "", "control socket filename")
	flag.Parse()
	cfg := &zmemif.PortCfg{
		Id:       0,
		Name:     "memif_c0",
		IsServer: false,
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
	fmt.Printf("%s", port)
	for {
		select {
		case err := <-ctrlSock.ErrChan:
			if err != nil {
				logrus.Fatal(err)
			}
		default:
			//fmt.Printf("%s", port)
			time.Sleep(2 * time.Second)
		}
	}

}
