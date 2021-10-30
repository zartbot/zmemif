package zmemif

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// memoryRegion represents a shared memory mapped file
type memoryRegion struct {
	data               []byte
	size               uint64
	fd                 int
	packetBufferOffset uint32
}

// memfdCreate returns memory file file descriptor (memif.sys_memfd_create)
func memfdCreate() (mfd int, err error) {
	p0, err := syscall.BytePtrFromString("memif_region_0")
	if err != nil {
		return -1, fmt.Errorf("memfdCreate: %s", err)
	}

	u_mfd, _, errno := syscall.Syscall(sys_memfd_create, uintptr(unsafe.Pointer(p0)), uintptr(mfd_allow_sealing), uintptr(0))
	if errno != 0 {
		return -1, fmt.Errorf("memfdCreate: %s", os.NewSyscallError("memfd_create", errno))
	}

	return int(u_mfd), nil
}

// initializeRegions initializes port regions (client only)
func (p *Port) initializeRegions() (err error) {

	err = p.addRegion(true, true)
	if err != nil {
		return fmt.Errorf("initializeRegions: %s", err)
	}

	return nil
}

// initializeQueues initializes port queues (client only)
func (p *Port) initializeQueues() (err error) {
	var q *Queue
	var desc descBuf
	var slot int

	desc = newDescBuf()
	desc.setFlags(0)
	desc.setRegion(0)
	desc.setLength(int(p.run.PacketBufferSize))

	for qid := 0; qid < int(p.run.NumQueuePairs); qid++ {
		/* TX */
		q = &Queue{
			ring:     p.newRing(0, ringTypeS2M, qid),
			lastHead: 0,
			lastTail: 0,
			port:     p,
		}
		q.ring.setCookie(cookie)
		q.ring.setFlags(1)
		q.interruptFd, err = eventFd()
		if err != nil {
			return err
		}
		q.putRing()
		p.txQueues = append(p.txQueues, *q)

		for j := 0; j < q.ring.size; j++ {
			slot = qid*q.ring.size + j
			desc.setOffset(int(p.regions[0].packetBufferOffset + uint32(slot)*p.run.PacketBufferSize))
			q.putDescBuf(slot, desc)
		}
	}
	for qid := 0; qid < int(p.run.NumQueuePairs); qid++ {
		/* RX */
		q = &Queue{
			ring:     p.newRing(0, ringTypeM2S, qid),
			lastHead: 0,
			lastTail: 0,
			port:     p,
		}
		q.ring.setCookie(cookie)
		q.ring.setFlags(1)
		q.interruptFd, err = eventFd()
		if err != nil {
			return err
		}
		q.putRing()
		p.rxQueues = append(p.rxQueues, *q)

		for j := 0; j < q.ring.size; j++ {
			slot = qid*q.ring.size + j
			desc.setOffset(int(p.regions[0].packetBufferOffset + uint32(slot)*p.run.PacketBufferSize))
			q.putDescBuf(slot, desc)
		}
	}

	return nil
}

// addRegions creates and adds a new memory region to the interface (client only)
func (p *Port) addRegion(hasPacketBuffers bool, hasRings bool) (err error) {
	var r memoryRegion

	if hasRings {
		r.packetBufferOffset = uint32((p.run.NumQueuePairs + p.run.NumQueuePairs) * (ringSize + descSize*(1<<p.run.Log2RingSize)))
	} else {
		r.packetBufferOffset = 0
	}

	if hasPacketBuffers {
		r.size = uint64(r.packetBufferOffset + p.run.PacketBufferSize*uint32(1<<p.run.Log2RingSize)*uint32(p.run.NumQueuePairs+p.run.NumQueuePairs))
	} else {
		r.size = uint64(r.packetBufferOffset)
	}

	r.fd, err = memfdCreate()
	if err != nil {
		return err
	}

	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(r.fd), uintptr(f_add_seals), uintptr(f_seal_shrink))
	if errno != 0 {
		syscall.Close(r.fd)
		return fmt.Errorf("memfdCreate: %s", os.NewSyscallError("fcntl", errno))
	}

	err = syscall.Ftruncate(r.fd, int64(r.size))
	if err != nil {
		syscall.Close(r.fd)
		r.fd = -1
		return fmt.Errorf("memfdCreate: %s", err)
	}

	r.data, err = syscall.Mmap(r.fd, 0, int(r.size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("addRegion: %s", err)
	}

	p.regions = append(p.regions, r)

	return nil
}
