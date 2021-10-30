package zmemif

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const maxEpollEvents = 1
const maxControlLen = 256
const errorFdNotFound = "fd not found"

const cookie = 0x3E31F20

// VersionMajor is memif protocols major version
const VersionMajor = 2

// VersionMinor is memif protocols minor version
const VersionMinor = 0

// Version is memif protocols version as uint16
// (M-Major m-minor: MMMMMMMMmmmmmmmm)
const Version = ((VersionMajor << 8) | VersionMinor)

type msgType uint16

const (
	msgTypeNone msgType = iota
	msgTypeAck
	msgTypeHello
	msgTypeInit
	msgTypeAddRegion
	msgTypeAddRing
	msgTypeConnect
	msgTypeConnected
	msgTypeDisconnect
)

const msgSize = 128
const msgTypeSize = 2
const msgAddRingFlagS2M = (1 << 0)

type MsgHello struct {
	// app name
	Name            [32]byte
	VersionMin      uint16
	VersionMax      uint16
	MaxRegion       uint16
	MaxRingM2S      uint16
	MaxRingS2M      uint16
	MaxLog2RingSize uint8
}

type MsgInit struct {
	Version uint16
	Id      uint32
	Mode    portMode
	Secret  [24]byte
	// app name
	Name [32]byte
}

type MsgAddRegion struct {
	Index uint16
	Size  uint64
}

type MsgAddRing struct {
	Flags          uint16
	Index          uint16
	Region         uint16
	Offset         uint32
	RingSizeLog2   uint8
	PrivateHdrSize uint16
}

type MsgConnect struct {
	// interface name
	Name [32]byte
}

type MsgConnected struct {
	// interface name
	Name [32]byte
}

type MsgDisconnect struct {
	Code   uint32
	String [96]byte
}

// controlMsg represents a message used in communication between memif peers
type controlMsg struct {
	Buffer *bytes.Buffer
	Fd     int
}

// listener represents a listener functionality of UNIX domain socket
type listener struct {
	socket *Socket
	event  syscall.EpollEvent
}

// controlChannel represents a communication channel between memif peers
// backed by UNIX domain socket
type controlChannel struct {
	listRef     *list.Element
	socket      *Socket
	port        *Port
	event       syscall.EpollEvent
	data        [msgSize]byte
	control     [maxControlLen]byte
	controlLen  int
	msgQueue    []controlMsg
	isConnected bool
}

// sendMsg sends a control message from contorl channels message queue
func (cc *controlChannel) sendMsg() (err error) {
	if len(cc.msgQueue) < 1 {
		return nil
	}
	// Get message buffer
	msg := cc.msgQueue[0]
	// Dequeue
	cc.msgQueue = cc.msgQueue[1:]

	iov := &syscall.Iovec{
		Base: &msg.Buffer.Bytes()[0],
		Len:  msgSize,
	}

	msgh := syscall.Msghdr{
		Iov:    iov,
		Iovlen: 1,
	}

	if msg.Fd > 0 {
		oob := syscall.UnixRights(msg.Fd)
		msgh.Control = &oob[0]
		msgh.Controllen = uint64(syscall.CmsgSpace(4))
	}

	_, _, errno := syscall.Syscall(syscall.SYS_SENDMSG, uintptr(cc.event.Fd), uintptr(unsafe.Pointer(&msgh)), uintptr(0))
	if errno != 0 {
		os.NewSyscallError("sendmsg", errno)
		return fmt.Errorf("SYS_SENDMSG: %s", errno)
	}

	return nil
}

// handleEvent handles epoll event for listener
func (l *listener) handleEvent(event *syscall.EpollEvent) error {
	// hang up
	if (event.Events & syscall.EPOLLHUP) == syscall.EPOLLHUP {
		err := l.close()
		if err != nil {
			return fmt.Errorf("failed to close listener after hang up event: %v", err)
		}
		return fmt.Errorf("hang up: %s", l.socket.filename)
	}

	// error
	if (event.Events & syscall.EPOLLERR) == syscall.EPOLLERR {
		err := l.close()
		if err != nil {
			return fmt.Errorf("failed to close listener after receiving an error event: %v", err)
		}
		return fmt.Errorf("reeceived error event on listener %s", l.socket.filename)
	}

	// read message
	if (event.Events & syscall.EPOLLIN) == syscall.EPOLLIN {
		newFd, _, err := syscall.Accept(int(l.event.Fd))
		if err != nil {
			return fmt.Errorf("accept: %s", err)
		}

		cc, err := l.socket.addControlChannel(newFd, nil)
		if err != nil {
			return fmt.Errorf("failed to add control channel: %s", err)
		}

		err = cc.msgEnqHello()
		if err != nil {
			return fmt.Errorf("msgEnqHello: %s", err)
		}

		err = cc.sendMsg()
		if err != nil {
			return err
		}

		return nil
	}

	return fmt.Errorf("unexpected event: %d", event.Events)
}

// handleEvent handles epoll event for control channel
func (cc *controlChannel) handleEvent(event *syscall.EpollEvent) error {
	var size int
	var err error

	// hang up
	if (event.Events & syscall.EPOLLHUP) == syscall.EPOLLHUP {
		// close cc, don't send msg
		err := cc.close(false, "")
		if err != nil {
			return fmt.Errorf("failed to close control channel after hang up event: %v", err)
		}
		return fmt.Errorf("hang up: %v", cc.port.GetName())
	}

	if (event.Events & syscall.EPOLLERR) == syscall.EPOLLERR {
		// close cc, don't send msg
		err := cc.close(false, "")
		if err != nil {
			return fmt.Errorf("failed to close control channel after receiving an error event: %v", err)
		}
		return fmt.Errorf("received error event on control channel %v", cc.port.GetName())
	}

	if (event.Events & syscall.EPOLLIN) == syscall.EPOLLIN {
		size, cc.controlLen, _, _, err = syscall.Recvmsg(int(cc.event.Fd), cc.data[:], cc.control[:], 0)
		if err != nil {
			return fmt.Errorf("recvmsg: %s", err)
		}
		if size != msgSize {
			return fmt.Errorf("invalid message size %d", size)
		}

		err = cc.parseMsg()
		if err != nil {
			return err
		}

		err = cc.sendMsg()
		if err != nil {
			return err
		}

		return nil
	}

	return fmt.Errorf("unexpected event: %v", event.Events)
}

// close closes the listener
func (l *listener) close() error {
	err := l.socket.delEvent(&l.event)
	if err != nil {
		return fmt.Errorf("failed to del event: %v", err)
	}
	err = syscall.Close(int(l.event.Fd))
	if err != nil {
		return fmt.Errorf("failed to close socket: %v", err)
	}
	return nil
}

// close closes a control channel, if the control channel is assigned an
// interface, the interface is disconnected
func (cc *controlChannel) close(sendMsg bool, str string) (err error) {
	if sendMsg {
		// first clear message queue so that the disconnect
		// message is the only message in queue
		cc.msgQueue = []controlMsg{}
		cc.msgEnqDisconnect(str)

		err = cc.sendMsg()
		if err != nil {
			return err
		}
	}

	err = cc.socket.delEvent(&cc.event)
	if err != nil {
		return fmt.Errorf("failed to del event: %v", err)
	}

	// remove referance form socket
	cc.socket.ccList.Remove(cc.listRef)

	if cc.port != nil {
		err = cc.port.disconnect()
		if err != nil {
			return fmt.Errorf("port Disconnect: %v", err)
		}
	}

	return nil
}

// AddListener adds a lisntener to the socket. The fd must describe a
// UNIX domain socket already bound to a UNIX domain filename and
// marked as listener
func (socket *Socket) AddListener(fd int) (err error) {
	l := &listener{
		// we will need this to look up master interface by id
		socket: socket,
	}

	l.event = syscall.EpollEvent{
		Events: syscall.EPOLLIN | syscall.EPOLLERR | syscall.EPOLLHUP,
		Fd:     int32(fd),
	}
	err = socket.addEvent(&l.event)
	if err != nil {
		return fmt.Errorf("failed to add event: %v", err)
	}

	socket.listener = l

	return nil
}

// addListener creates new UNIX domain socket, binds it to the address
// and marks it as listener
func (socket *Socket) addListener() (err error) {
	// create socket
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		return fmt.Errorf("failed to create UNIX domain socket")
	}
	usa := &syscall.SockaddrUnix{Name: socket.filename}

	// Bind to address and start listening
	err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_PASSCRED, 1)
	if err != nil {
		return fmt.Errorf("failed to set socket option %s : %v", socket.filename, err)
	}
	err = syscall.Bind(fd, usa)
	if err != nil {
		return fmt.Errorf("failed to bind socket %s : %v", socket.filename, err)
	}
	err = syscall.Listen(fd, syscall.SOMAXCONN)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s : %v", socket.filename, err)
	}

	return socket.AddListener(fd)
}

//addControlChannel returns a new controlChannel and adds it to the socket
func (socket *Socket) addControlChannel(fd int, p *Port) (*controlChannel, error) {
	cc := &controlChannel{
		socket:      socket,
		port:        p,
		isConnected: false,
	}

	var err error

	cc.event = syscall.EpollEvent{
		Events: syscall.EPOLLIN | syscall.EPOLLERR | syscall.EPOLLHUP,
		Fd:     int32(fd),
	}
	err = socket.addEvent(&cc.event)
	if err != nil {
		return nil, fmt.Errorf("failed to add event: %v", err)
	}

	cc.listRef = socket.ccList.PushBack(cc)

	return cc, nil
}

func (cc *controlChannel) msgEnqAck() (err error) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeAck)

	msg := controlMsg{
		Buffer: buf,
		Fd:     -1,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) msgEnqHello() (err error) {
	hello := MsgHello{
		VersionMin:      Version,
		VersionMax:      Version,
		MaxRegion:       255,
		MaxRingM2S:      255,
		MaxRingS2M:      255,
		MaxLog2RingSize: 14,
	}

	copy(hello.Name[:], []byte(cc.socket.appName))

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeHello)
	binary.Write(buf, binary.LittleEndian, hello)

	msg := controlMsg{
		Buffer: buf,
		Fd:     -1,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) parseHello() (err error) {
	var hello MsgHello

	buf := bytes.NewReader(cc.data[msgTypeSize:])
	err = binary.Read(buf, binary.LittleEndian, &hello)
	if err != nil {
		return
	}

	if hello.VersionMin > Version || hello.VersionMax < Version {
		return fmt.Errorf("incompatible memif version")
	}

	cc.port.run = cc.port.cfg.MemoryConfig

	cc.port.run.NumQueuePairs = min16(cc.port.cfg.MemoryConfig.NumQueuePairs, hello.MaxRingS2M)
	cc.port.run.NumQueuePairs = min16(cc.port.cfg.MemoryConfig.NumQueuePairs, hello.MaxRingM2S)
	cc.port.run.Log2RingSize = min8(cc.port.cfg.MemoryConfig.Log2RingSize, hello.MaxLog2RingSize)

	cc.port.remoteName = string(hello.Name[:])

	return nil
}

func (cc *controlChannel) msgEnqInit() (err error) {
	init := MsgInit{
		Version: Version,
		Id:      cc.port.cfg.Id,
		Mode:    portModeEthernet,
	}

	copy(init.Name[:], []byte(cc.socket.appName))

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeInit)
	binary.Write(buf, binary.LittleEndian, init)

	msg := controlMsg{
		Buffer: buf,
		Fd:     -1,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) parseInit() (err error) {
	var init MsgInit

	buf := bytes.NewReader(cc.data[msgTypeSize:])
	err = binary.Read(buf, binary.LittleEndian, &init)
	if err != nil {
		return
	}

	if init.Version != Version {
		return fmt.Errorf("incompatible memif driver version")
	}

	// find peer port
	for elt := cc.socket.portList.Front(); elt != nil; elt = elt.Next() {
		port, ok := elt.Value.(*Port)
		if ok {
			if port.cfg.Id == init.Id && port.cfg.IsServer && port.cc == nil {
				// verify secret
				if port.cfg.Secret != init.Secret {
					return fmt.Errorf("invalid secret")
				}
				// interface is assigned to control channel
				port.cc = cc
				cc.port = port
				cc.port.run = cc.port.cfg.MemoryConfig
				cc.port.remoteName = string(init.Name[:])

				return nil
			}
		}
	}

	return fmt.Errorf("invalid interface id")
}

func (cc *controlChannel) msgEnqAddRegion(regionIndex uint16) (err error) {
	if len(cc.port.regions) <= int(regionIndex) {
		return fmt.Errorf("invalid region index")
	}

	addRegion := MsgAddRegion{
		Index: regionIndex,
		Size:  cc.port.regions[regionIndex].size,
	}

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeAddRegion)
	binary.Write(buf, binary.LittleEndian, addRegion)

	msg := controlMsg{
		Buffer: buf,
		Fd:     cc.port.regions[regionIndex].fd,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) parseAddRegion() (err error) {
	var addRegion MsgAddRegion

	buf := bytes.NewReader(cc.data[msgTypeSize:])
	err = binary.Read(buf, binary.LittleEndian, &addRegion)
	if err != nil {
		return
	}

	fd, err := cc.parseControlMsg()
	if err != nil {
		return fmt.Errorf("parseControlMsg: %s", err)
	}

	if addRegion.Index > 255 {
		return fmt.Errorf("invalid memory region index")
	}

	region := memoryRegion{
		size: addRegion.Size,
		fd:   fd,
	}

	cc.port.regions = append(cc.port.regions, region)

	return nil
}

func (cc *controlChannel) msgEnqAddRing(ringType ringType, ringIndex uint16) (err error) {
	var q Queue
	var flags uint16 = 0

	if ringType == ringTypeS2M {
		q = cc.port.txQueues[ringIndex]
		flags = msgAddRingFlagS2M
	} else {
		q = cc.port.rxQueues[ringIndex]
	}

	addRing := MsgAddRing{
		Index:          ringIndex,
		Offset:         uint32(q.ring.offset),
		Region:         uint16(q.ring.region),
		RingSizeLog2:   uint8(q.ring.log2Size),
		Flags:          flags,
		PrivateHdrSize: 0,
	}

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeAddRing)
	binary.Write(buf, binary.LittleEndian, addRing)

	msg := controlMsg{
		Buffer: buf,
		Fd:     q.interruptFd,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) parseAddRing() (err error) {
	var addRing MsgAddRing

	buf := bytes.NewReader(cc.data[msgTypeSize:])
	err = binary.Read(buf, binary.LittleEndian, &addRing)
	if err != nil {
		return
	}

	fd, err := cc.parseControlMsg()
	if err != nil {
		return err
	}

	if addRing.Index >= cc.port.run.NumQueuePairs {
		return fmt.Errorf("invalid ring index")
	}

	q := Queue{
		port:        cc.port,
		interruptFd: fd,
	}

	if (addRing.Flags & msgAddRingFlagS2M) == msgAddRingFlagS2M {
		q.ring = newRing(int(addRing.Region), ringTypeS2M, int(addRing.Offset), int(addRing.RingSizeLog2))
		cc.port.rxQueues = append(cc.port.rxQueues, q)
	} else {
		q.ring = newRing(int(addRing.Region), ringTypeM2S, int(addRing.Offset), int(addRing.RingSizeLog2))
		cc.port.txQueues = append(cc.port.txQueues, q)
	}

	return nil
}

func (cc *controlChannel) msgEnqConnect() (err error) {
	var connect MsgConnect
	copy(connect.Name[:], []byte(cc.port.cfg.Name))

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeConnect)
	binary.Write(buf, binary.LittleEndian, connect)

	msg := controlMsg{
		Buffer: buf,
		Fd:     -1,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) parseConnect() (err error) {
	var connect MsgConnect

	buf := bytes.NewReader(cc.data[msgTypeSize:])
	err = binary.Read(buf, binary.LittleEndian, &connect)
	if err != nil {
		return
	}

	cc.port.peerName = string(connect.Name[:])

	err = cc.port.connect()
	if err != nil {
		return err
	}

	cc.isConnected = true

	return nil
}

func (cc *controlChannel) msgEnqConnected() (err error) {
	var connected MsgConnected
	copy(connected.Name[:], []byte(cc.port.cfg.Name))

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeConnected)
	binary.Write(buf, binary.LittleEndian, connected)

	msg := controlMsg{
		Buffer: buf,
		Fd:     -1,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) parseConnected() (err error) {
	var conn MsgConnected

	buf := bytes.NewReader(cc.data[msgTypeSize:])
	err = binary.Read(buf, binary.LittleEndian, &conn)
	if err != nil {
		return
	}

	cc.port.peerName = string(conn.Name[:])

	err = cc.port.connect()
	if err != nil {
		return err
	}

	cc.isConnected = true

	return nil
}

func (cc *controlChannel) msgEnqDisconnect(str string) (err error) {
	dc := MsgDisconnect{
		// not implemented
		Code: 0,
	}
	copy(dc.String[:], str)

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgTypeDisconnect)
	binary.Write(buf, binary.LittleEndian, dc)

	msg := controlMsg{
		Buffer: buf,
		Fd:     -1,
	}

	cc.msgQueue = append(cc.msgQueue, msg)

	return nil
}

func (cc *controlChannel) parseDisconnect() (err error) {
	var dc MsgDisconnect

	buf := bytes.NewReader(cc.data[msgTypeSize:])
	err = binary.Read(buf, binary.LittleEndian, &dc)
	if err != nil {
		return
	}

	err = cc.close(false, string(dc.String[:]))
	if err != nil {
		return fmt.Errorf("failed to disconnect control channel: %v", err)
	}

	return nil
}

func (cc *controlChannel) parseMsg() error {
	var msgType msgType
	var err error

	buf := bytes.NewReader(cc.data[:])
	binary.Read(buf, binary.LittleEndian, &msgType)

	if msgType == msgTypeAck {
		return nil
	} else if msgType == msgTypeHello {
		// Configure
		err = cc.parseHello()
		if err != nil {
			goto error
		}
		// Initialize client memif
		err = cc.port.initializeRegions()
		if err != nil {
			goto error
		}
		err = cc.port.initializeQueues()
		if err != nil {
			goto error
		}
		// Enqueue messages
		err = cc.msgEnqInit()
		if err != nil {
			goto error
		}
		for i := 0; i < len(cc.port.regions); i++ {
			err = cc.msgEnqAddRegion(uint16(i))
			if err != nil {
				goto error
			}
		}
		for i := 0; uint16(i) < cc.port.run.NumQueuePairs; i++ {
			err = cc.msgEnqAddRing(ringTypeS2M, uint16(i))
			if err != nil {
				goto error
			}
		}
		for i := 0; uint16(i) < cc.port.run.NumQueuePairs; i++ {
			err = cc.msgEnqAddRing(ringTypeM2S, uint16(i))
			if err != nil {
				goto error
			}
		}
		err = cc.msgEnqConnect()
		if err != nil {
			goto error
		}
	} else if msgType == msgTypeInit {
		err = cc.parseInit()
		if err != nil {
			goto error
		}

		err = cc.msgEnqAck()
		if err != nil {
			goto error
		}
	} else if msgType == msgTypeAddRegion {
		err = cc.parseAddRegion()
		if err != nil {
			goto error
		}

		err = cc.msgEnqAck()
		if err != nil {
			goto error
		}
	} else if msgType == msgTypeAddRing {
		err = cc.parseAddRing()
		if err != nil {
			goto error
		}

		err = cc.msgEnqAck()
		if err != nil {
			goto error
		}
	} else if msgType == msgTypeConnect {
		err = cc.parseConnect()
		if err != nil {
			goto error
		}

		err = cc.msgEnqConnected()
		if err != nil {
			goto error
		}
	} else if msgType == msgTypeConnected {
		err = cc.parseConnected()
		if err != nil {
			goto error
		}
	} else if msgType == msgTypeDisconnect {
		err = cc.parseDisconnect()
		if err != nil {
			goto error
		}
	} else {
		err = fmt.Errorf("unknown message %d", msgType)
		goto error
	}

	return nil

error:
	fmt.Printf("parseMsg Error: %s\n", err)
	err1 := cc.close(true, err.Error())
	if err1 != nil {
		return fmt.Errorf(err.Error(), ": Failed to close control channel: ", err1)
	}

	return err
}

// parseControlMsg parses control message and returns file descriptor
// if any
func (cc *controlChannel) parseControlMsg() (fd int, err error) {
	// Assert only called when we require FD
	fd = -1

	controlMsgs, err := syscall.ParseSocketControlMessage(cc.control[:cc.controlLen])
	if err != nil {
		return -1, fmt.Errorf("syscall.ParseSocketControlMessage: %s", err)
	}

	if len(controlMsgs) == 0 {
		return -1, fmt.Errorf("missing control message")
	}

	for _, cmsg := range controlMsgs {
		if cmsg.Header.Level == syscall.SOL_SOCKET {
			if cmsg.Header.Type == syscall.SCM_RIGHTS {
				FDs, err := syscall.ParseUnixRights(&cmsg)
				if err != nil {
					return -1, fmt.Errorf("syscall.ParseUnixRights: %s", err)
				}
				if len(FDs) == 0 {
					continue
				}
				// Only expect single FD
				fd = FDs[0]
			}
		}
	}

	if fd == -1 {
		return -1, fmt.Errorf("missing file descriptor")
	}

	return fd, nil
}
