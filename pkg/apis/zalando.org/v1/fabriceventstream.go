package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FabricEventStream defines FabricEventStream Custom Resource Definition Object.
type FabricEventStream struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec FabricEventStreamSpec `json:"spec"`
}

// FabricEventStreamSpec defines the specification for the FabricEventStream TPR.
type FabricEventStreamSpec struct {
	ApplicationId string        `json:"applicationId"`
	EventStreams  []EventStream `json:"eventStreams"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FabricEventStreamList defines a list of FabricEventStreams .
type FabricEventStreamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FabricEventStream `json:"items"`
}

// EventStream defines the source, flow and sink of the event stream
type EventStream struct {
	EventStreamFlow   EventStreamFlow   `json:"flow"`
	EventStreamSink   EventStreamSink   `json:"sink"`
	EventStreamSource EventStreamSource `json:"source"`
}

// EventStreamFlow defines the flow characteristics of the event stream
type EventStreamFlow struct {
	Type          string  `json:"type"`
	PayloadColumn *string `json:"payloadColumn,omitempty"`
}

// EventStreamSink defines the target of the event stream
type EventStreamSink struct {
	Type         string  `json:"type"`
	EventType    string  `json:"eventType,omitempty"`
	MaxBatchSize *uint32 `json:"maxBatchSize,omitempty"`
}

// EventStreamSource defines the source of the event stream and connection for FES operator
type EventStreamSource struct {
	Type             string           `json:"type"`
	Schema           string           `json:"schema,omitempty" defaults:"public"`
	EventStreamTable EventStreamTable `json:"table"`
	Filter           *string          `json:"filter,omitempty"`
	Connection       Connection       `json:"jdbcConnection"`
}

// EventStreamTable defines the name and ID column to be used for streaming
type EventStreamTable struct {
	Name     string  `json:"name"`
	IDColumn *string `json:"idColumn,omitempty"`
}

// Connection to be used for allowing the FES operator to connect to a database
type Connection struct {
	Url             string  `json:"jdbcUrl"`
	SlotName        string  `json:"slotName"`
	PluginType      string  `json:"pluginType,omitempty"`
	PublicationName *string `json:"publicationName,omitempty"`
	DBAuth          DBAuth  `json:"databaseAuthentication"`
}

// DBAuth specifies the credentials to be used for connecting with the database
type DBAuth struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	UserKey     string `json:"userKey,omitempty"`
	PasswordKey string `json:"passwordKey,omitempty"`
}
