package zmemif

import (
	"encoding/binary"
)

const descSize = 16

// next buffer present
const descFlagNext = (1 << 0)

// desc field offsets
const descFlagsOffset = 0
const descRegionOffset = 2
const descLengthOffset = 4
const descOffsetOffset = 8
const descMetadataOffset = 12

// descBuf represents a memif descriptor as array of bytes
type descBuf []byte

// newDescBuf returns new descriptor buffer
func newDescBuf() descBuf {
	return make(descBuf, descSize)
}

// getDescBuff copies descriptor from shared memory to descBuf
func (q *Queue) getDescBuf(slot int, db descBuf) {
	copy(db, q.port.regions[q.ring.region].data[q.ring.offset+ringSize+slot*descSize:])
	//fmt.Printf("flag:%d\tRegion:%d\tLen:%d\tOffset:%d\n", db.getFlags(), db.getRegion(), db.getLength(), db.getOffset())
}

// putDescBuf copies contents of descriptor buffer into shared memory
func (q *Queue) putDescBuf(slot int, db descBuf) {
	copy(q.port.regions[q.ring.region].data[q.ring.offset+ringSize+slot*descSize:], db)
}

func (db descBuf) getFlags() int {
	return (int)(binary.LittleEndian.Uint16((db)[descFlagsOffset:]))
}

func (db descBuf) getRegion() int {
	return (int)(binary.LittleEndian.Uint16((db)[descRegionOffset:]))
}

func (db descBuf) getLength() int {
	return (int)(binary.LittleEndian.Uint32((db)[descLengthOffset:]))
}

func (db descBuf) getOffset() int {
	return (int)(binary.LittleEndian.Uint32((db)[descOffsetOffset:]))
}

func (db descBuf) setFlags(val int) {
	binary.LittleEndian.PutUint16((db)[descFlagsOffset:], uint16(val))
}

func (db descBuf) setRegion(val int) {
	binary.LittleEndian.PutUint16((db)[descRegionOffset:], uint16(val))
}

func (db descBuf) setLength(val int) {
	binary.LittleEndian.PutUint32((db)[descLengthOffset:], uint32(val))
}

func (db descBuf) setOffset(val int) {
	binary.LittleEndian.PutUint32((db)[descOffsetOffset:], uint32(val))
}

func (db descBuf) getMetadata() int {
	return (int)(binary.LittleEndian.Uint32((db)[descMetadataOffset:]))
}

func (db descBuf) setMetadata(val int) {
	binary.LittleEndian.PutUint32((db)[descMetadataOffset:], uint32(val))
}
