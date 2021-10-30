package zmemif

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"
)

// Queue represents rx or tx queue
type Queue struct {
	ring        *ring
	port        *Port
	lastHead    uint16
	lastTail    uint16
	interruptFd int
}

// GetEventFd returns queues interrupt event fd
func (q *Queue) GetEventFd() (int, error) {
	return q.interruptFd, nil
}

// close closes the queue
func (q *Queue) close() {
	syscall.Close(q.interruptFd)
}

// readHead reads ring head directly form the shared memory
func (q *Queue) readHead() (head int) {
	return (int)(*(*uint16)(unsafe.Pointer(&q.port.regions[q.ring.region].data[q.ring.offset+ringHeadOffset])))
	// return atomicload16(&q.port.regions[q.region].data[q.offset + descHeadOffset])
}

// readTail reads ring tail directly form the shared memory
func (q *Queue) readTail() (tail int) {
	return (int)(*(*uint16)(unsafe.Pointer(&q.port.regions[q.ring.region].data[q.ring.offset+ringTailOffset])))
	// return atomicload16(&q.port.regions[q.region].data[q.offset + descTailOffset])
}

// writeHead writes ring head directly to the shared memory
func (q *Queue) writeHead(value int) {
	*(*uint16)(unsafe.Pointer(&q.port.regions[q.ring.region].data[q.ring.offset+ringHeadOffset])) = *(*uint16)(unsafe.Pointer(&value))
	//atomicstore16(&q.port.regions[q.region].data[q.offset + descHeadOffset], value)
}

// writeTail writes ring tail directly to the shared memory
func (q *Queue) writeTail(value int) {
	*(*uint16)(unsafe.Pointer(&q.port.regions[q.ring.region].data[q.ring.offset+ringTailOffset])) = *(*uint16)(unsafe.Pointer(&value))
	//atomicstore16(&q.port.regions[q.region].data[q.offset + descTailOffset], value)
}

func (q *Queue) setDescLength(slot int, length int) {
	*(*uint16)(unsafe.Pointer(&q.port.regions[q.ring.region].data[q.ring.offset+ringSize+slot*descSize+descLengthOffset])) = *(*uint16)(unsafe.Pointer(&length))
}

// getFlags reads ring flags directly from the shared memory
func (q *Queue) getFlags() int {
	return (int)(*(*uint16)(unsafe.Pointer(&q.port.regions[q.ring.region].data[q.ring.offset+ringFlagsOffset])))
}

// isInterrupt returns true if the queue is in interrupt mode
func (q *Queue) isInterrupt() bool {
	return (q.getFlags() & ringFlagInterrupt) == 0
}

// interrupt performs an interrupt if the queue is in interrupt mode
func (q *Queue) interrupt() error {
	if q.isInterrupt() {
		buf := make([]byte, 8)
		binary.PutUvarint(buf, 1)
		n, err := syscall.Write(q.interruptFd, buf[:])
		if err != nil {
			return err
		}
		if n != 8 {
			return fmt.Errorf("faild to write to eventfd")
		}
	}

	return nil
}
