package zmemif

import "encoding/binary"

type ringType uint8

const (
	ringTypeS2M ringType = iota
	ringTypeM2S
)

const ringSize = 128
const ringFlagInterrupt = 0

// ring field offsets
const ringCookieOffset = 0
const ringFlagsOffset = 4
const ringHeadOffset = 6
const ringTailOffset = 64

// ringBuf represents a memif ring as array of bytes
type ringBuf []byte

type ring struct {
	ringType ringType
	size     int
	log2Size int
	region   int
	rb       ringBuf
	offset   int
}

// newRing returns new memif ring based on data received in msgAddRing (master only)
func newRing(regionIndex int, ringType ringType, ringOffset int, log2RingSize int) *ring {
	r := &ring{
		ringType: ringType,
		size:     (1 << log2RingSize),
		log2Size: log2RingSize,
		rb:       make(ringBuf, ringSize),
		offset:   ringOffset,
	}

	return r
}

// newRing returns a new memif ring
func (p *Port) newRing(regionIndex int, ringType ringType, ringIndex int) *ring {
	r := &ring{
		ringType: ringType,
		size:     (1 << p.run.Log2RingSize),
		log2Size: int(p.run.Log2RingSize),
		rb:       make(ringBuf, ringSize),
	}

	rSize := ringSize + descSize*r.size
	if r.ringType == ringTypeS2M {
		r.offset = 0
	} else {
		r.offset = int(p.run.NumQueuePairs) * rSize
	}
	r.offset += ringIndex * rSize

	return r
}

// putRing put the ring to the shared memory
func (q *Queue) putRing() {
	copy(q.port.regions[q.ring.region].data[q.ring.offset:], q.ring.rb)
}

// updateRing updates ring with data from shared memory
func (q *Queue) updateRing() {
	copy(q.ring.rb, q.port.regions[q.ring.region].data[q.ring.offset:])
}

func (r *ring) getCookie() int {
	return (int)(binary.LittleEndian.Uint32((r.rb)[ringCookieOffset:]))
}

// getFlags returns the flags value from ring buffer
// Use Queue.getFlags in fast-path to avoid updating the whole ring.
func (r *ring) getFlags() int {
	return (int)(binary.LittleEndian.Uint16((r.rb)[ringFlagsOffset:]))
}

// getHead returns the head pointer value from ring buffer.
// Use readHead in fast-path to avoid updating the whole ring.
func (r *ring) getHead() int {
	return (int)(binary.LittleEndian.Uint16((r.rb)[ringHeadOffset:]))
}

// getTail returns the tail pointer value from ring buffer.
// Use readTail in fast-path to avoid updating the whole ring.
func (r *ring) getTail() int {
	return (int)(binary.LittleEndian.Uint16((r.rb)[ringTailOffset:]))
}

func (r *ring) setCookie(val int) {
	binary.LittleEndian.PutUint32((r.rb)[ringCookieOffset:], uint32(val))
}

func (r *ring) setFlags(val int) {
	binary.LittleEndian.PutUint16((r.rb)[ringFlagsOffset:], uint16(val))
}

// setHead set the head pointer value int the ring buffer.
// Use writeHead in fast-path to avoid putting the whole ring into shared memory.
func (r *ring) setHead(val int) {
	binary.LittleEndian.PutUint16((r.rb)[ringHeadOffset:], uint16(val))
}

// setTail set the tail pointer value int the ring buffer.
// Use writeTail in fast-path to avoid putting the whole ring into shared memory.
func (r *ring) setTail(val int) {
	binary.LittleEndian.PutUint16((r.rb)[ringTailOffset:], uint16(val))
}
