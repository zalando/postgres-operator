package ringlog

import (
	"container/list"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

type RingLogger interface {
	Insert(level logrus.Level, t time.Time, workerId *uint32, clusterName spec.NamespacedName, message string) error
	Walk() []*LogEntry
}

type RingLog struct {
	size int
	list *list.List
	mu   *sync.RWMutex
}

type LogEntry struct {
	Time        time.Time
	Level       logrus.Level
	ClusterName spec.NamespacedName
	Worker      *uint32
	Message     string
}

func (e LogEntry) MarshalJSON() ([]byte, error) {
	v := map[string]interface{}{
		"Time":        e.Time,
		"Level":       e.Level.String(),
		"ClusterName": e.ClusterName,
		"Message":     e.Message,
	}
	if e.Worker != nil {
		v["Worker"] = fmt.Sprintf("%d", *e.Worker)
	}

	return json.Marshal(v)
}

func New(size int) *RingLog {
	r := RingLog{
		list: list.New(),
		size: size,
		mu:   &sync.RWMutex{},
	}

	return &r
}

func (r *RingLog) Insert(level logrus.Level, t time.Time, workerId *uint32, clusterName spec.NamespacedName, message string) error {
	l := &LogEntry{
		Time:        t,
		Level:       level,
		ClusterName: clusterName,
		Worker:      workerId,
		Message:     message,
	}

	func() {
		r.mu.Lock()
		defer r.mu.Unlock()

		r.list.PushBack(l)
		if r.list.Len() > r.size {
			r.list.Remove(r.list.Front())
		}
	}()

	return nil
}

func (r *RingLog) Walk() []*LogEntry {
	res := make([]*LogEntry, r.size)

	r.mu.RLock()
	defer r.mu.RUnlock()

	st := r.list.Front()
	for i := 0; i < r.size; i++ {
		if st == nil {
			continue
		}
		res[i] = (st.Value).(*LogEntry)
		st = st.Next()
	}

	return res
}
