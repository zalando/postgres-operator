package spec

import (
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"
)

type PodEventType string

type PodName types.NamespacedName

const (
	PodEventAdd    PodEventType = "ADD"
	PodEventUpdate PodEventType = "UPDATE"
	PodEventDelete PodEventType = "DELETE"
)

type PodEvent struct {
	ClusterName ClusterName
	PodName     PodName
	PrevPod     *v1.Pod
	CurPod      *v1.Pod
	EventType   PodEventType
}

func (p PodName) String() string {
	return types.NamespacedName(p).String()
}

type ClusterName types.NamespacedName

func (c ClusterName) String() string {
	return types.NamespacedName(c).String()
}

type PgUser struct {
	Name     string
	Password string
	Flags    []string
	MemberOf string
}
