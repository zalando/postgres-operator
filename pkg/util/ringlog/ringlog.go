package ringlog

import (
	"container/list"
	"sync"
)

// RingLogger describes ring logger methods
type RingLogger interface {
	Insert(interface{})
	Walk() []interface{}
}

// RingLog is a capped logger with fixed size
type RingLog struct {
	size int
	list *list.List
	mu   *sync.RWMutex
}

// New creates new Ring logger
func New(size int) *RingLog {
	r := RingLog{
		list: list.New(),
		size: size,
		mu:   &sync.RWMutex{},
	}

	return &r
}

// Insert inserts new LogEntry into the ring logger
func (r *RingLog) Insert(obj interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.list.PushBack(obj)
	if r.list.Len() > r.size {
		r.list.Remove(r.list.Front())
	}
}

// Walk dumps all the LogEntries from the Ring logger
func (r *RingLog) Walk() []interface{} {
	res := make([]interface{}, 0)

	r.mu.RLock()
	defer r.mu.RUnlock()

	st := r.list.Front()
	for i := 0; i < r.size; i++ {
		if st == nil {
			return res
		}
		res = append(res, st.Value)
		st = st.Next()
	}

	return res
}
