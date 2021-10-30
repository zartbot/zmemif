/*
 *------------------------------------------------------------------
 * Copyright (c) 2020 Cisco and/or its affiliates.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at:
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *------------------------------------------------------------------
 */

// Package memif provides the implementation of shared memory interface (memif).
//
// Memif network interfaces communicate using UNIX domain socket. This socket
// must be first created using NewSocket(). Then interfaces can be added
// to this socket using NewInterface(). To start communication on each socket
// socket.StartPolling() must be called. socket.StopPolling() will stop
// the communication. When the interface changes link status Connected and
// Disconencted callbacks set in Arguments for each interface are called
// respectively. Once the interface is connected rx and tx queues can be
// aquired using interface.GetRxQueue() and interface.GetTxQueue().
// Packets can be transmitted by calling queue.ReadPacket() on rx queues and
// queue.WritePacket() on tx queues. If the interface is disconnected
// queue.ReadPacket() and queue.WritePacket() MUST not be called.
//
// Data transmission is backed by shared memory. The driver works in
// promiscuous mode only.

package zmemif

import (
	"fmt"
	"syscall"
)

// IsServer returns true if the interfaces role is server, else returns false
func (p *Port) IsServer() bool {
	return p.cfg.IsServer
}

// GetRemoteName returns the name of the application on which the peer
// interface exists
func (p *Port) GetRemoteName() string {
	return p.remoteName
}

// GetPeerName returns peer interfaces name
func (p *Port) GetPeerName() string {
	return p.peerName
}

// GetName returens interfaces name
func (p *Port) GetName() string {
	return p.cfg.Name
}

// GetMemoryConfig returns interfaces active memory config.
// If Port is not connected the config is invalid.
func (p *Port) GetMemoryConfig() MemoryConfig {
	return p.run
}

// GetRxQueue returns an rx queue specified by queue index
func (p *Port) GetRxQueue(qid int) (*Queue, error) {
	if qid >= len(p.rxQueues) {
		return nil, fmt.Errorf("invalid Queue index")
	}
	return &p.rxQueues[qid], nil
}

// GetRxQueue returns a tx queue specified by queue index
func (p *Port) GetTxQueue(qid int) (*Queue, error) {
	if qid >= len(p.txQueues) {
		return nil, fmt.Errorf("invalid Queue index")
	}
	return &p.txQueues[qid], nil
}

// GetSocket returns the socket the interface belongs to
func (p *Port) GetSocket() *Socket {
	return p.socket
}

// GetExtendData returns interfaces extend data
func (p *Port) GetExtendData() interface{} {
	return p.cfg.ExtendData
}

// GetId returns interfaces id
func (p *Port) GetId() uint32 {
	return p.cfg.Id
}

// RoleToString returns 'Server' if isServer os true, else returns 'Client'
func RoleToString(isServer bool) string {
	if isServer {
		return "Server"
	}
	return "Client"
}

// IsConnecting returns true if the port is connecting
func (p *Port) IsConnecting() bool {
	return p.cc != nil
}

// IsConnected returns true if the port is connected
func (p *Port) IsConnected() bool {
	if p.cc != nil && p.cc.isConnected {
		return true
	}
	return false
}

// Disconnect disconnects the port
func (p *Port) Disconnect() (err error) {
	if p.cc != nil {
		// close control and disconenct port
		return p.cc.close(true, "Port disconnected")
	}
	return nil
}

// Delete deletes the port
func (p *Port) Delete() (err error) {
	p.Disconnect()
	// remove referance on socket
	p.socket.portList.Remove(p.listRef)
	p = nil

	return nil
}

// RequestConnection is used by client port to connect to a socket and
// create a control channel
func (p *Port) RequestConnection() error {
	if p.IsServer() {
		return fmt.Errorf("only client can request connection")
	}
	// create socket
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		return fmt.Errorf("failed to create UNIX domain socket: %v", err)
	}
	usa := &syscall.SockaddrUnix{Name: p.socket.filename}

	// Connect to listener socket
	err = syscall.Connect(fd, usa)
	if err != nil {
		return fmt.Errorf("failed to connect socket %s : %v", p.socket.filename, err)
	}

	// Create control channel
	p.cc, err = p.socket.addControlChannel(fd, p)
	if err != nil {
		return fmt.Errorf("failed to create control channel: %v", err)
	}
	return nil
}

// connect finalizes interface connection
func (p *Port) connect() (err error) {
	for rid := range p.regions {
		r := &p.regions[rid]
		if r.data == nil {
			r.data, err = syscall.Mmap(r.fd, 0, int(r.size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
			if err != nil {
				return fmt.Errorf("mmap: %s", err)
			}
		}
	}

	for _, q := range p.txQueues {
		q.updateRing()

		if q.ring.getCookie() != cookie {
			return fmt.Errorf("wrong cookie")
		}

		q.lastHead = 0
		q.lastTail = 0
	}

	for _, q := range p.rxQueues {
		q.updateRing()

		if q.ring.getCookie() != cookie {
			return fmt.Errorf("wrong cookie")
		}

		q.lastHead = 0
		q.lastTail = 0
	}

	return p.cfg.ConnectedFunc(p)
}

// disconnect finalizes port disconnection
func (p *Port) disconnect() (err error) {
	if p.cc == nil { // disconnected
		return nil
	}
	err = p.cfg.DisconnectedFunc(p)
	if err != nil {
		return fmt.Errorf("disconnectedFunc: %v", err)
	}

	for _, q := range p.txQueues {
		q.close()
	}
	p.txQueues = []Queue{}

	for _, q := range p.rxQueues {
		q.close()
	}
	p.rxQueues = []Queue{}

	// unmap regions
	for _, r := range p.regions {
		err = syscall.Munmap(r.data)
		if err != nil {
			return err
		}
		err = syscall.Close(r.fd)
		if err != nil {
			return err
		}
	}
	p.regions = nil
	p.cc = nil

	p.peerName = ""
	p.remoteName = ""

	return nil
}

// defaultDisconnectedFunc
func defaultDisconnectedFunc(p *Port) error {
	fmt.Println("Disconnected: ", p.GetName())
	close(p.QuitChan) // stop polling
	close(p.ErrChan)
	p.Wg.Wait() // wait until polling stops, then continue disconnect
	return nil
}

func (p *Port) String() string {
	result := fmt.Sprintf("%s:\n\trole: %s\n\tid: %d\n",
		p.GetName(), RoleToString(p.IsServer()), p.GetId())
	link := "down"
	if p.IsConnected() {
		link = "up"
	}
	result += fmt.Sprintf("\tlink:%s\n\tremote: %s\n\tpeer: %s\n",
		link, p.GetRemoteName(), p.GetPeerName())
	if p.IsConnected() {
		mc := p.GetMemoryConfig()
		result += fmt.Sprintf("queue pairs: %d\nring size: %d\nbuffer size: %d\n",
			mc.NumQueuePairs, (1 << mc.Log2RingSize), mc.PacketBufferSize)
	}
	return result
}
