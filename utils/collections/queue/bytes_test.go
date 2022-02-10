package queue

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBytesQueue
func TestBytesQueueGrowAndShrink(t *testing.T) {
	b := []byte{1, 2, 3, 4}
	queue := NewBytesQueue()
	queue.PushTail(b)
	queue.PushTail(b)
	queue.PushTail(b)
	queue.PushTail(b)
	queue.PushTail(b)
	assert.Equal(t, 5, len(queue.buffer))

	queue.PushTail(b) // the cap will grow to 10
	assert.Equal(t, 10, len(queue.buffer))

	queue.PushTail(b)
	assert.Equal(t, 0, queue.head)
	assert.Equal(t, 6, queue.tail)
	assert.Equal(t, 7, queue.size)

	queue.PopHead()
	assert.Equal(t, 1, queue.head)
	assert.Equal(t, 6, queue.tail)
	assert.Equal(t, 6, queue.size)

	queue.PopHead() // shrink happens after exec
	assert.Equal(t, 0, queue.head)
	assert.Equal(t, 4, queue.tail)
	assert.Equal(t, 5, queue.size)
	assert.Equal(t, 7, len(queue.buffer)) // new cap is 7

	queue.PushTail(b)
	queue.PopHead()
	queue.PushTail(b)
	queue.PopHead()
	assert.Equal(t, 2, queue.head)
	assert.Equal(t, 6, queue.tail)
	assert.Equal(t, 5, queue.size)

	queue.PushTail(b)
	queue.PopHead()
	queue.PushTail(b)
	queue.PopHead()
	assert.Equal(t, 4, queue.head)
	assert.Equal(t, 1, queue.tail)
	assert.Equal(t, 5, queue.size)

	queue.PopHead()
	assert.Equal(t, 5, queue.head)
	assert.Equal(t, 1, queue.tail)
	assert.Equal(t, 4, queue.size)

	queue.PopHead() // will shrink
	assert.Equal(t, 0, queue.head)
	assert.Equal(t, 2, queue.tail)
	assert.Equal(t, 3, queue.size)
	assert.Equal(t, 5, len(queue.buffer)) // new cap is 5
}

// TestBytesQueue2 the index will remain same
func TestBytesQueueSizeStayUnchanged(t *testing.T) {
	b := []byte{1, 2, 3, 4}
	queue := NewBytesQueue()
	head := queue.head
	queue.PushTail(b)
	queue.PopHead()
	queue.PushTail(b)
	queue.PopHead()
	assert.Equal(t, 0, queue.size)
	assert.Equal(t, queue.head, head)

	queue.PushTail(b)
	assert.Equal(t, 0, queue.head)
	assert.Equal(t, 0, queue.tail)
	assert.Equal(t, 1, queue.size)

	queue.PushTail(b)
	queue.PopHead()
	assert.Equal(t, 1, queue.head)
	assert.Equal(t, 1, queue.tail)
	assert.Equal(t, 1, queue.size)

	queue.PopHead()
	queue.PushTail(b)
	queue.PopHead()
	queue.PushTail(b)
	assert.Equal(t, 1, queue.head)
	assert.Equal(t, 1, queue.tail)
	assert.Equal(t, 1, queue.size)

	queue.PushTail(b)
	queue.PopHead()
	queue.PushTail(b)
	queue.PopHead()
	queue.PushTail(b)
	queue.PopHead()
	assert.Equal(t, 4, queue.head)
	assert.Equal(t, 4, queue.tail)
	assert.Equal(t, 1, queue.size)
	queue.PushTail(b)
	queue.PopHead()
	assert.Equal(t, 0, queue.head)
	assert.Equal(t, 0, queue.tail)
	assert.Equal(t, 1, queue.size)
}

// TestBytesQueueModifyHead .
func TestBytesQueueModifyHead(t *testing.T) {
	bytes := func() []byte {
		return []byte{1, 2, 3, 4}
	}
	queue := NewBytesQueue()
	queue.PushTail(bytes())
	queue.PushTail(bytes())

	head := queue.PeekHead()
	*head = (*head)[2:]

	head = queue.PeekHead()
	assert.Equal(t, byte(3), (*head)[0])
	assert.Equal(t, byte(4), (*head)[1])

	he := queue.PopHead()
	assert.Equal(t, byte(3), he[0])
	assert.Equal(t, byte(4), he[1])

	assert.Equal(t, 4, len(queue.PopHead()))
}

// TestLargeScaleGrowAndShrink .
func TestLargeScaleGrowAndShrink(t *testing.T) {
	bytes := func() []byte {
		return []byte{1, 2, 3, 4}
	}

	queue := NewBytesQueue()
	push := func(n int) {
		for n > 0 {
			queue.PushTail(bytes())
			n--
		}
	}
	pop := func(n int) {
		for n > 0 {
			queue.PopHead()
			n--
		}
	}

	push(1023)
	queue.PushTail([]byte{1, 2, 3, 4, 5})
	pop(1023)

	assert.Equal(t, 1, queue.Size())
	assert.Equal(t, 5, len(queue.buffer))
	assert.Equal(t, 5, len(queue.PopHead()))

	queue.PopHead()
	assert.Equal(t, 0, queue.Size())
	queue.PushTail(nil)
	assert.Equal(t, 0, queue.Size())
}
