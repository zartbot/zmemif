package main

import (
	"flag"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zartbot/zmemif"

	_ "net/http/pprof"
)

var queueNum uint16 = 2
var portNum uint32 = 16
var serverMode bool = true

func recvpkt(p *zmemif.Port, qid int) {
	p.Wg.Add(1)
	defer p.Wg.Done()

	data := p.ExtendData.(*PortStats)
	pkt := make([]byte, 2048)
	rxq, err := p.GetRxQueue(qid)
	if err != nil {
		logrus.Fatal("Get RX-Queue failed.")
	}

	for {
		pktLen, _ := rxq.ReadPacket(pkt)
		if pktLen > 0 {
			atomic.AddUint64(data.PacketCnt, 1)
		}

	}
}

func Connected(p *zmemif.Port) error {
	fmt.Println("Connected: ", p.GetName())
	for i := 0; i < int(queueNum); i++ {
		go recvpkt(p, 0)
	}

	return nil
}

type PortStats struct {
	PacketCnt *uint64
}

func main() {
	socketName := flag.String("socket", "", "control socket filename")
	flag.Parse()

	var pktCnt uint64 = 0

	ctrlSock, err := zmemif.NewSocket("foo", *socketName)
	if err != nil {
		logrus.Fatal("create socket failed: %v", err)
	}

	for ifindex := uint32(0); ifindex < portNum; ifindex++ {

		ifName := fmt.Sprintf("memif_s%d", ifindex)
		cfg := &zmemif.PortCfg{
			Id:       ifindex,
			Name:     ifName,
			IsServer: serverMode,
			MemoryConfig: zmemif.MemoryConfig{
				NumQueuePairs: queueNum,
			},
			ConnectedFunc: Connected,
			ExtendData: &PortStats{
				PacketCnt: &pktCnt,
			},
		}
		port, err := zmemif.NewPort(ctrlSock, cfg, nil)
		if err != nil {
			logrus.Fatal(err)
		}
		fmt.Printf("%s", port)
	}

	ctrlSock.StartPolling()

	for {
		select {
		case err := <-ctrlSock.ErrChan:
			if err != nil {
				logrus.Fatal(err)
			}
		default:
			pps := atomic.LoadUint64(&pktCnt)
			logrus.Info("pps: ", pps)
			atomic.StoreUint64(&pktCnt, 0)
			time.Sleep(1 * time.Second)
		}
	}

}
