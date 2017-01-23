package spec

import(
    "encoding/json"

    "k8s.io/client-go/pkg/api/meta"
    "k8s.io/client-go/pkg/api/unversioned"
    "k8s.io/client-go/pkg/api"
)

type Pgconf struct {
    Parameter string `json:"param"`
    Value     string `json:"value"`
}

type SpiloSpec struct {
    EtcdHost              string   `json:"etcd_host"`
    VolumeSize            int      `json:"volume_size"`
    NumberOfInstances     int32    `json:"number_of_instances"`
    DockerImage           string   `json:"docker_image"`
    PostgresConfiguration []Pgconf `json:"postgres_configuration"`
    ResourceCPU           string   `json:"resource_cpu"`
    ResourceMemory        string   `json:"resource_memory"`
}

type Spilo struct {
    unversioned.TypeMeta `json:",inline"`
    Metadata             api.ObjectMeta `json:"metadata"`
    Spec                 SpiloSpec      `json:"spec"`
}

type SpiloList struct {
    unversioned.TypeMeta `json:",inline"`
    Metadata             unversioned.ListMeta `json:"metadata"`
    Items                []Spilo              `json:"items"`
}

func (s *Spilo) GetObjectKind() unversioned.ObjectKind {
    return &s.TypeMeta
}

func (s *Spilo) GetObjectMeta() meta.Object {
    return &s.Metadata
}
func (sl *SpiloList) GetObjectKind() unversioned.ObjectKind {
    return &sl.TypeMeta
}

func (sl *SpiloList) GetListMeta() unversioned.List {
    return &sl.Metadata
}

type SpiloListCopy SpiloList
type SpiloCopy Spilo

func (e *Spilo) UnmarshalJSON(data []byte) error {
    tmp := SpiloCopy{}
    err := json.Unmarshal(data, &tmp)
    if err != nil {
        return err
    }
    tmp2 := Spilo(tmp)
    *e = tmp2

    return nil
}

func (el *SpiloList) UnmarshalJSON(data []byte) error {
    tmp := SpiloListCopy{}
    err := json.Unmarshal(data, &tmp)
    if err != nil {
        return err
    }
    tmp2 := SpiloList(tmp)
    *el = tmp2

    return nil
}
