package queue

import (
	"github.com/projecteru2/systemd-runtime/utils/math"
)

const initCap = 5
const expandFactor float32 = 1.5
const shrinkTriggerFactor float32 = 0.5
const shrinkFactor float32 = 0.7

// BytesQueue elements in the queue is non nil
type BytesQueue struct {
	buffer [][]byte
	size   int
	head   int
	tail   int
}

func NewBytesQueue() BytesQueue {
	b := BytesQueue{
		buffer: alloc(initCap),
	}
	return b
}

func (q *BytesQueue) PushTail(bytes []byte) {
	if bytes == nil {
		return
	}
	if q.size == 0 {
		q.buffer[q.tail] = bytes
		q.size++
		return
	}

	q.ensureCapacity(q.size + 1)
	tail := q.nextTail()
	q.buffer[tail] = bytes
	q.tail = tail
	q.size++
}

func (q *BytesQueue) PeekHead() *[]byte {
	if q.Empty() {
		return nil
	}
	return &q.buffer[q.head]
}

func (q *BytesQueue) PopHead() []byte {
	if q.Empty() {
		return nil
	}
	r := q.buffer[q.head]
	if q.size == 1 {
		q.size = 0
		return r
	}
	q.head = q.nextHead()
	q.size--
	q.shrink()
	return r
}

func (q *BytesQueue) Size() int {
	return q.size
}

func (q *BytesQueue) Empty() bool {
	return q.size == 0
}

func (q *BytesQueue) ensureCapacity(newCap int) {
	cap := len(q.buffer)
	if newCap > cap {
		q.expand(newCap)
	}
}

func (q *BytesQueue) expand(minCap int) {
	size := len(q.buffer)
	q.uncheckedRealloc(math.MaxInt(minCap, math.MaxInt(size+5, int(float32(size)*expandFactor))))
}

func (q *BytesQueue) shrink() {
	cap := len(q.buffer)
	if cap <= initCap {
		return
	}
	if q.size > int(float32(cap)*shrinkTriggerFactor) {
		return
	}
	newCap := int(float32(cap) * shrinkFactor)
	q.uncheckedRealloc(math.MaxInt(initCap, newCap))
}

func (q *BytesQueue) uncheckedRealloc(newCap int) {
	buffer := alloc(newCap)
	if q.tail >= q.head {
		copy(buffer[:q.size], q.buffer[q.head:q.tail+1])
		q.buffer = buffer
		q.head = 0
		q.tail = q.size - 1
		return
	}
	cap := len(q.buffer)
	idx := cap - q.head
	copy(buffer[:idx], q.buffer[q.head:])
	copy(buffer[idx:q.size], q.buffer[0:q.tail+1])
	q.head = 0
	q.tail = q.size - 1
	q.buffer = buffer
}

func (q *BytesQueue) nextHead() int {
	return (q.head + 1) % len(q.buffer)
}

func (q *BytesQueue) nextTail() int {
	return (q.tail + 1) % len(q.buffer)
}

func alloc(size int) [][]byte {
	return make([][]byte, size)
}
