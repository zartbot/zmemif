package zmemif

import "fmt"

// ReadPacket reads one packet form the shared memory and
// returns the number of bytes read
func (q *Queue) ReadPacket(pkt []byte) (int, error) {
	var mask int = q.ring.size - 1
	var slot int
	var lastSlot int
	var length int
	var offset int
	var pktOffset int = 0
	var nSlots uint16
	var desc descBuf = newDescBuf()

	if q.port.cfg.IsServer {
		slot = int(q.lastHead)
		lastSlot = q.readHead()
	} else {
		slot = int(q.lastTail)
		lastSlot = q.readTail()
	}

	nSlots = uint16(lastSlot - slot)
	if nSlots == 0 {
		goto refill
	}

	// copy descriptor from shm
	q.getDescBuf(slot&mask, desc)
	length = desc.getLength()
	offset = desc.getOffset()

	copy(pkt[:], q.port.regions[desc.getRegion()].data[offset:offset+length])
	pktOffset += length

	slot++
	nSlots--

	for (desc.getFlags() & descFlagNext) == descFlagNext {
		if nSlots == 0 {
			return 0, fmt.Errorf("incomplete chained buffer, may suggest peer error")
		}

		q.getDescBuf(slot&mask, desc)
		length = desc.getLength()
		offset = desc.getOffset()

		copy(pkt[pktOffset:], q.port.regions[desc.getRegion()].data[offset:offset+length])
		pktOffset += length

		slot++
		nSlots--
	}

refill:
	if q.port.cfg.IsServer {
		q.lastHead = uint16(slot)
		q.writeTail(slot)
	} else {
		q.lastTail = uint16(slot)

		head := q.readHead()

		for nSlots := uint16(q.ring.size - head + int(q.lastTail)); nSlots > 0; nSlots-- {
			q.setDescLength(head&mask, int(q.port.run.PacketBufferSize))
			head++
		}
		q.writeHead(head)
	}

	return pktOffset, nil
}

// WritePacket writes one packet to the shared memory and
// returns the number of bytes written
func (q *Queue) WritePacket(pkt []byte) int {
	var mask int = q.ring.size - 1
	var slot int
	var nFree uint16
	var packetBufferSize int = int(q.port.run.PacketBufferSize)

	if q.port.cfg.IsServer {
		slot = q.readTail()
		nFree = uint16(q.readHead() - slot)
	} else {
		slot = q.readHead()
		nFree = uint16(q.ring.size - slot + q.readTail())
	}

	if nFree == 0 {
		q.interrupt()
		return 0
	}

	// copy descriptor from shm
	desc := newDescBuf()
	q.getDescBuf(slot&mask, desc)
	// reset flags
	desc.setFlags(0)
	// reset length
	if q.port.cfg.IsServer {
		packetBufferSize = desc.getLength()
	}
	desc.setLength(0)
	offset := desc.getOffset()

	// write packet into memif buffer
	n := copy(q.port.regions[desc.getRegion()].data[offset:offset+packetBufferSize], pkt[:])
	desc.setLength(n)
	for n < len(pkt) {
		nFree--
		if nFree == 0 {
			q.interrupt()
			return 0
		}
		desc.setFlags(descFlagNext)
		q.putDescBuf(slot&mask, desc)
		slot++

		// copy descriptor from shm
		q.getDescBuf(slot&mask, desc)
		// reset flags
		desc.setFlags(0)
		// reset length
		if q.port.cfg.IsServer {
			packetBufferSize = desc.getLength()
		}
		desc.setLength(0)
		offset := desc.getOffset()

		tmp := copy(q.port.regions[desc.getRegion()].data[offset:offset+packetBufferSize], pkt[:])
		desc.setLength(tmp)
		n += tmp
	}

	// copy descriptor to shm
	q.putDescBuf(slot&mask, desc)
	slot++

	if q.port.cfg.IsServer {
		q.writeTail(slot)
	} else {
		q.writeHead(slot)
	}

	q.interrupt()

	return n
}
