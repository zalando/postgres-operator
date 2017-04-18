package spec

import (
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"
)

type PodEventType string

type NamespacedName types.NamespacedName

const (
	PodEventAdd    PodEventType = "ADD"
	PodEventUpdate PodEventType = "UPDATE"
	PodEventDelete PodEventType = "DELETE"
)

type PodEvent struct {
	ClusterName NamespacedName
	PodName     NamespacedName
	PrevPod     *v1.Pod
	CurPod      *v1.Pod
	EventType   PodEventType
}

type PgUser struct {
	Name     string
	Password string
	Flags    []string
	MemberOf string
}

func (p NamespacedName) String() string {
	return types.NamespacedName(p).String()
}

func (n *NamespacedName) Decode(value string) error {
	name := types.NewNamespacedNameFromString(value)
	if value != "" && name == (types.NamespacedName{}) {
		name.Name = value
		name.Namespace = v1.NamespaceDefault
	}

	*n = NamespacedName(name)

	return nil
}
