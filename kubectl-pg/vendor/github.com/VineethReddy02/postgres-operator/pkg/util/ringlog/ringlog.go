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
	sync.RWMutex
	size int
	list *list.List
}

// New creates new Ring logger
func New(size int) *RingLog {
	r := RingLog{
		list: list.New(),
		size: size,
	}

	return &r
}

// Insert inserts new entry into the ring logger
func (r *RingLog) Insert(obj interface{}) {
	r.Lock()
	defer r.Unlock()

	r.list.PushBack(obj)
	if r.list.Len() > r.size {
		r.list.Remove(r.list.Front())
	}
}

// Walk dumps all the entries from the Ring logger
func (r *RingLog) Walk() []interface{} {
	res := make([]interface{}, 0)

	r.RLock()
	defer r.RUnlock()

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
