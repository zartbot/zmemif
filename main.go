package zmemif

import (
	"container/list"
	"fmt"
	"sync"
	"syscall"
)

const (
	DefaultSocketFilename   = "/tmp/memif.sock"
	DefaultNumQueuePairs    = 1
	DefaultLog2RingSize     = 10
	DefaultPacketBufferSize = 2048
)

type portMode uint8

const (
	portModeEthernet portMode = iota
	portModeIp
	portModePuntInject
)

const mfd_allow_sealing = 2
const sys_memfd_create = 319
const f_add_seals = 1033
const f_seal_shrink = 0x0002

const efd_nonblock = 04000

// Port represents memif network interface
type Port struct {
	cfg        PortCfg
	run        MemoryConfig
	ExtendData interface{}
	listRef    *list.Element
	socket     *Socket
	cc         *controlChannel
	remoteName string
	peerName   string
	regions    []memoryRegion
	txQueues   []Queue
	rxQueues   []Queue
	ErrChan    chan error
	QuitChan   chan struct{}
	Wg         sync.WaitGroup
}

// ConnectedFunc is a callback called when an interface is connected
type ConnectedFunc func(p *Port) error

// DisconnectedFunc is a callback called when an interface is disconnected
type DisconnectedFunc func(p *Port) error

// MemoryConfig represents shared memory configuration
type MemoryConfig struct {
	NumQueuePairs    uint16 // number of queue pairs
	Log2RingSize     uint8  // ring size as log2
	PacketBufferSize uint32 // size of single packet buffer
}

// PortCfg represent port configuration
type PortCfg struct {
	Id               uint32 // Port identifier unique across socket. Used to identify peer Port when connecting
	IsServer         bool   // Port role server/client
	Name             string
	Secret           [24]byte // optional parameter, secrets of the Ports must match if they are to connect
	MemoryConfig     MemoryConfig
	ConnectedFunc    ConnectedFunc    // callback called when Port changes status to connected
	DisconnectedFunc DisconnectedFunc // callback called when Port changes status to disconnected
	ExtendData       interface{}      // ExtendData used by client program
}

// NewSocket returns a new Socket
func NewSocket(appName string, filename string) (socket *Socket, err error) {
	socket = &Socket{
		appName:  appName,
		filename: filename,
		portList: list.New(),
		ccList:   list.New(),
		ErrChan:  make(chan error, 1),
	}
	if socket.filename == "" {
		socket.filename = DefaultSocketFilename
	}

	socket.epfd, _ = syscall.EpollCreate1(0)

	efd, _ := eventFd()
	socket.wakeEvent = syscall.EpollEvent{
		Events: syscall.EPOLLIN | syscall.EPOLLERR | syscall.EPOLLHUP,
		Fd:     int32(efd),
	}
	err = socket.addEvent(&socket.wakeEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to add event: %v", err)
	}

	return socket, nil
}

func NewPort(socket *Socket, cfg *PortCfg, extendData interface{}) (*Port, error) {
	port, err := socket.NewPort(cfg)

	if err != nil {
		return nil, fmt.Errorf("failed to create interface on socket %s: %s", socket.GetFilename(), err)
	}

	// client attempts to connect to control socket
	// to handle control communication call socket.StartPolling()
	if !port.IsServer() {
		fmt.Println(cfg.Name, ": Connecting to control socket...")
		for !port.IsConnecting() {
			err = port.RequestConnection()
			if err != nil {
				/* TODO: check for ECONNREFUSED errno
				 * if error is ECONNREFUSED it may simply mean that master
				 * interface is not up yet, use i.RequestConnection()
				 */
				return nil, fmt.Errorf("faild to connect: %v", err)
			}
		}
	}
	return port, nil
}
