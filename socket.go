package zmemif

import (
	"container/list"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
)

// Socket represents a UNIX domain socket used for communication
// between memif peers
type Socket struct {
	appName      string
	filename     string
	listener     *listener
	portList     *list.List
	ccList       *list.List
	epfd         int
	wakeEvent    syscall.EpollEvent
	stopPollChan chan struct{}
	wg           sync.WaitGroup
	ErrChan      chan error
}

// handleEvent handles epoll event
func (socket *Socket) handleEvent(event *syscall.EpollEvent) error {
	if socket.listener != nil && socket.listener.event.Fd == event.Fd {
		return socket.listener.handleEvent(event)
	}

	for elt := socket.ccList.Front(); elt != nil; elt = elt.Next() {
		cc, ok := elt.Value.(*controlChannel)
		if ok {
			if cc.event.Fd == event.Fd {
				return cc.handleEvent(event)
			}
		}
	}

	return fmt.Errorf(errorFdNotFound)
}

// GetFilename returns sockets filename
func (socket *Socket) GetFilename() string {
	return socket.filename
}

// StopPolling stops polling events on the socket
func (socket *Socket) StopPolling() error {
	if socket.stopPollChan != nil {
		// stop polling msg
		close(socket.stopPollChan)
		// wake epoll
		buf := make([]byte, 8)
		binary.PutUvarint(buf, 1)
		n, err := syscall.Write(int(socket.wakeEvent.Fd), buf[:])
		if err != nil {
			return err
		}
		if n != 8 {
			return fmt.Errorf("faild to write to eventfd")
		}
		// wait until polling is stopped
		socket.wg.Wait()
	}

	return nil
}

// StartPolling starts polling and handling events on the socket,
// enabling communication between memif peers
func (socket *Socket) StartPolling() {
	socket.stopPollChan = make(chan struct{})
	socket.wg.Add(1)
	go func() {
		var events [maxEpollEvents]syscall.EpollEvent
		defer socket.wg.Done()

		for {
			select {
			case <-socket.stopPollChan:
				return
			default:
				num, err := syscall.EpollWait(socket.epfd, events[:], -1)
				if err != nil {
					socket.ErrChan <- fmt.Errorf("epollWait: %v", err)
					return
				}

				for ev := 0; ev < num; ev++ {
					if events[0].Fd == socket.wakeEvent.Fd {
						continue
					}
					err = socket.handleEvent(&events[0])
					if err != nil {
						socket.ErrChan <- fmt.Errorf("handleEvent: %v", err)
					}
				}
			}
		}
	}()
}

// NewPort returns a new memif network port. When creating an port
// it's id must be unique across socket with the exception of loopback interface
// in which case the id is the same but role differs
func (socket *Socket) NewPort(cfg *PortCfg) (*Port, error) {
	var err error
	// make sure the ID is unique on this socket
	for elt := socket.portList.Front(); elt != nil; elt = elt.Next() {
		p, ok := elt.Value.(*Port)
		if ok {
			if p.cfg.Id == cfg.Id && p.cfg.IsServer == cfg.IsServer {
				return nil, fmt.Errorf("port with id %d role %s already exists on this socket", cfg.Id, RoleToString(cfg.IsServer))
			}
		}
	}

	// copy interface configuration
	p := Port{
		cfg: *cfg,
	}
	// set default values
	if p.cfg.MemoryConfig.NumQueuePairs == 0 {
		p.cfg.MemoryConfig.NumQueuePairs = DefaultNumQueuePairs
	}

	if p.cfg.MemoryConfig.NumQueuePairs > 8 {
		logrus.Warn("queue pairs number > 8 may cause race condition, please use multiple interface instead")
	}
	if p.cfg.MemoryConfig.Log2RingSize == 0 {
		p.cfg.MemoryConfig.Log2RingSize = DefaultLog2RingSize
	}
	if p.cfg.MemoryConfig.PacketBufferSize == 0 {
		p.cfg.MemoryConfig.PacketBufferSize = DefaultPacketBufferSize
	}

	p.socket = socket
	p.ExtendData = cfg.ExtendData

	if p.cfg.DisconnectedFunc == nil {
		p.cfg.DisconnectedFunc = defaultDisconnectedFunc
	}

	p.ErrChan = make(chan error, 1)
	p.QuitChan = make(chan struct{}, 1)

	// append port to the list
	p.listRef = socket.portList.PushBack(&p)

	if p.cfg.IsServer {
		if socket.listener == nil {
			err = socket.addListener()
			if err != nil {
				return nil, fmt.Errorf("failed to create listener channel: %s", err)
			}
		}
	}

	return &p, nil
}

// addEvent adds event to epoll instance associated with the socket
func (socket *Socket) addEvent(event *syscall.EpollEvent) error {
	err := syscall.EpollCtl(socket.epfd, syscall.EPOLL_CTL_ADD, int(event.Fd), event)
	if err != nil {
		return fmt.Errorf("EpollCtl: %s", err)
	}
	return nil
}

// addEvent deletes event to epoll instance associated with the socket
func (socket *Socket) delEvent(event *syscall.EpollEvent) error {
	err := syscall.EpollCtl(socket.epfd, syscall.EPOLL_CTL_DEL, int(event.Fd), event)
	if err != nil {
		return fmt.Errorf("EpollCtl: %s", err)
	}
	return nil
}

// Delete deletes the socket
func (socket *Socket) Delete() (err error) {
	for elt := socket.ccList.Front(); elt != nil; elt = elt.Next() {
		cc, ok := elt.Value.(*controlChannel)
		if ok {
			err = cc.close(true, "Socket deleted")
			if err != nil {
				return nil
			}
		}
	}
	for elt := socket.portList.Front(); elt != nil; elt = elt.Next() {
		i, ok := elt.Value.(*Port)
		if ok {
			err = i.Delete()
			if err != nil {
				return err
			}
		}
	}

	if socket.listener != nil {
		err = socket.listener.close()
		if err != nil {
			return err
		}
		err = os.Remove(socket.filename)
		if err != nil {
			return nil
		}
	}

	err = socket.delEvent(&socket.wakeEvent)
	if err != nil {
		return fmt.Errorf("failed to delete event: %v", err)
	}

	syscall.Close(socket.epfd)

	return nil
}
