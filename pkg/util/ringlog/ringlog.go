package ringlog

import (
	"container/list"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

// RingLogger describes ring logger methods
type RingLogger interface {
	Insert(level logrus.Level, t time.Time, workerID *uint32, clusterName spec.NamespacedName, message string) error
	Walk() []*LogEntry
}

// RingLog is a capped logger with fixed size
type RingLog struct {
	size int
	list *list.List
	mu   *sync.RWMutex
}

// LogEntry describes log entry in the RingLogger
type LogEntry struct {
	Time        time.Time
	Level       logrus.Level
	ClusterName spec.NamespacedName
	Worker      *uint32
	Message     string
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
func (r *RingLog) Insert(level logrus.Level, t time.Time, workerID *uint32, clusterName spec.NamespacedName, message string) error {
	l := &LogEntry{
		Time:        t,
		Level:       level,
		ClusterName: clusterName,
		Worker:      workerID,
		Message:     message,
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.list.PushBack(l)
	if r.list.Len() > r.size {
		r.list.Remove(r.list.Front())
	}

	return nil
}

// Walk dumps all the LogEntries from the Ring logger
func (r *RingLog) Walk() []*LogEntry {
	res := make([]*LogEntry, 0)

	r.mu.RLock()
	defer r.mu.RUnlock()

	st := r.list.Front()
	for i := 0; i < r.size; i++ {
		if st == nil {
			return res
		}
		res = append(res, (st.Value).(*LogEntry))
		st = st.Next()
	}

	return res
}
