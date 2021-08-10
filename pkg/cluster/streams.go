package cluster

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	zalandov1alpha1 "github.com/zalando/postgres-operator/pkg/apis/zalando.org/v1alpha1"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var outboxTableNameTemplate config.StringTemplate = "{table}_{eventtype}_outbox"

func (c *Cluster) createStreams() error {
	c.setProcessName("creating streams")

	fes := c.generateFabricEventStream()
	_, err := c.KubeClient.FabricEventStreamsGetter.FabricEventStreams(c.Namespace).Create(context.TODO(), fes, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("could not create event stream custom resource: %v", err)
	}

	return nil
}

func (c *Cluster) updateStreams(newEventStreams *zalandov1alpha1.FabricEventStream) error {
	c.setProcessName("updating event streams")

	_, err := c.KubeClient.FabricEventStreamsGetter.FabricEventStreams(c.Namespace).Update(context.TODO(), newEventStreams, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("could not update event stream custom resource: %v", err)
	}

	return nil
}

func (c *Cluster) syncPostgresConfig() error {

	desiredPostgresConfig := make(map[string]interface{})
	slots := make(map[string]map[string]string)

	c.logger.Debugf("setting wal level to 'logical' in postgres configuration")
	desiredPostgresConfig["postgresql"] = map[string]interface{}{patroniPGParametersParameterName: map[string]string{"wal_level": "logical"}}

	for _, stream := range c.Spec.Streams {
		slotName := c.getLogicalReplicationSlot(stream.Database)

		if slotName == "" {
			c.logger.Debugf("creating logical replication slot %q in database %q", constants.EventStreamSourceSlotPrefix+stream.Database, stream.Database)
			slot := map[string]string{
				"database": stream.Database,
				"plugin":   "wal2json",
				"type":     "logical",
			}
			slots[constants.EventStreamSourceSlotPrefix+stream.Database] = slot
		}
	}

	if len(slots) > 0 {
		desiredPostgresConfig["slots"] = slots
	} else {
		return nil
	}

	pods, err := c.listPods()
	if err != nil || len(pods) == 0 {
		return err
	}
	for i, pod := range pods {
		podName := util.NameFromMeta(pods[i].ObjectMeta)
		effectivePostgresConfig, err := c.patroni.GetConfig(&pod)
		if err != nil {
			c.logger.Warningf("could not get Postgres config from pod %s: %v", podName, err)
			continue
		}

		_, err = c.checkAndSetGlobalPostgreSQLConfiguration(&pod, effectivePostgresConfig, desiredPostgresConfig)
		if err != nil {
			c.logger.Warningf("could not set PostgreSQL configuration options for pod %s: %v", podName, err)
			continue
		}
	}

	return nil
}

func (c *Cluster) syncStreamDbResources() error {

	for _, stream := range c.Spec.Streams {
		if err := c.initDbConnWithName(stream.Database); err != nil {
			return fmt.Errorf("could not init connection to database %q specified for event stream: %v", stream.Database, err)
		}

		for table, eventType := range stream.Tables {
			tableName, schemaName := getTableSchema(table)
			if exists, err := c.tableExists(tableName, schemaName); !exists {
				return fmt.Errorf("could not find table %q specified for event stream: %v", table, err)
			}
			// check if outbox table exists and if not, create it
			outboxTable := outboxTableNameTemplate.Format("table", tableName, "eventtype", eventType)
			if exists, err := c.tableExists(outboxTable, schemaName); !exists {
				return fmt.Errorf("could not find outbox table %q specified for event stream: %v", outboxTable, err)
			}
		}
	}

	return nil
}

func (c *Cluster) generateFabricEventStream() *zalandov1alpha1.FabricEventStream {
	eventStreams := make([]zalandov1alpha1.EventStream, 0)

	for _, stream := range c.Spec.Streams {
		for table, eventType := range stream.Tables {
			streamSource := c.getEventStreamSource(stream, table, eventType)
			streamFlow := getEventStreamFlow(stream)
			streamSink := getEventStreamSink(stream, eventType)

			eventStreams = append(eventStreams, zalandov1alpha1.EventStream{
				EventStreamFlow:   streamFlow,
				EventStreamSink:   streamSink,
				EventStreamSource: streamSource})
		}
	}

	return &zalandov1alpha1.FabricEventStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:        c.Name + constants.FESsuffix,
			Namespace:   c.Namespace,
			Annotations: c.AnnotationsToPropagate(c.annotationsSet(nil)),
		},
		Spec: zalandov1alpha1.FabricEventStreamSpec{
			ApplicationId: "",
			EventStreams:  eventStreams,
		},
	}
}

func (c *Cluster) getEventStreamSource(stream acidv1.Stream, table, eventType string) zalandov1alpha1.EventStreamSource {
	streamFilter := stream.Filter[table]
	_, schema := getTableSchema(table)
	return zalandov1alpha1.EventStreamSource{
		Type:             constants.EventStreamSourcePGType,
		Schema:           schema,
		EventStreamTable: getOutboxTable(table, eventType),
		Filter:           streamFilter,
		Connection:       c.getStreamConnection(stream.Database, constants.EventStreamSourceSlotPrefix+constants.UserRoleNameSuffix),
	}
}

func getEventStreamFlow(stream acidv1.Stream) zalandov1alpha1.EventStreamFlow {
	switch stream.StreamType {
	case "nakadi":
		return zalandov1alpha1.EventStreamFlow{
			Type:           constants.EventStreamFlowPgNakadiType,
			DataTypeColumn: constants.EventStreamFlowDataTypeColumn,
			DataOpColumn:   constants.EventStreamFlowDataOpColumn,
			MetadataColumn: constants.EventStreamFlowMetadataColumn,
			DataColumn:     constants.EventStreamFlowDataColumn}
	case "sqs":
		return zalandov1alpha1.EventStreamFlow{
			Type:             constants.EventStreamFlowPgApiType,
			CallHomeIdColumn: "id",
			CallHomeUrl:      stream.SqsArn}
	}

	return zalandov1alpha1.EventStreamFlow{}
}

func getEventStreamSink(stream acidv1.Stream, eventType string) zalandov1alpha1.EventStreamSink {
	switch stream.StreamType {
	case "nakadi":
		return zalandov1alpha1.EventStreamSink{
			Type:         constants.EventStreamSinkNakadiType,
			EventType:    eventType,
			MaxBatchSize: stream.BatchSize}
	case "sqs":
		return zalandov1alpha1.EventStreamSink{
			Type:      constants.EventStreamSinkSqsType,
			QueueName: stream.QueueName}
	}

	return zalandov1alpha1.EventStreamSink{}
}

func getTableSchema(fullTableName string) (tableName, schemaName string) {
	schemaName = "public"
	tableName = fullTableName
	if strings.Contains(fullTableName, ".") {
		schemaName = strings.Split(fullTableName, ".")[0]
		tableName = strings.Split(fullTableName, ".")[1]
	}

	return tableName, schemaName
}

func getOutboxTable(tableName, eventType string) zalandov1alpha1.EventStreamTable {
	return zalandov1alpha1.EventStreamTable{
		Name:     outboxTableNameTemplate.Format("table", tableName, "eventtype", eventType),
		IDColumn: "id",
	}
}

func (c *Cluster) getStreamConnection(database, user string) zalandov1alpha1.Connection {
	return zalandov1alpha1.Connection{
		Url:      fmt.Sprintf("jdbc:postgresql://%s.%s/%s?user=%s&ssl=true&sslmode=require", c.Name, c.Namespace, database, user),
		SlotName: c.getLogicalReplicationSlot(database),
		DBAuth: zalandov1alpha1.DBAuth{
			Type:        constants.EventStreamSourceAuthType,
			Name:        c.credentialSecretNameForCluster(user, c.ClusterName),
			UserKey:     "username",
			PasswordKey: "password",
		},
	}
}

func (c *Cluster) getLogicalReplicationSlot(database string) string {
	for slotName, slot := range c.Spec.Patroni.Slots {
		if strings.HasPrefix(slotName, constants.EventStreamSourceSlotPrefix) && slot["type"] == "logical" && slot["database"] == database {
			return slotName
		}
	}

	return ""
}

func (c *Cluster) syncStreams() error {

	c.setProcessName("syncing streams")

	err := c.syncPostgresConfig()
	if err != nil {
		return fmt.Errorf("could not update Postgres config for event streaming: %v", err)
	}

	effectiveStreams, err := c.KubeClient.FabricEventStreamsGetter.FabricEventStreams(c.Namespace).Get(context.TODO(), c.Name+constants.FESsuffix, metav1.GetOptions{})
	if err != nil {
		if !k8sutil.ResourceNotFound(err) {
			return fmt.Errorf("error during reading of event streams: %v", err)
		}

		c.logger.Infof("event streams do not exist")
		err := c.createStreams()
		if err != nil {
			return fmt.Errorf("could not create missing streams: %v", err)
		}
	} else {
		err := c.syncStreamDbResources()
		if err != nil {
			c.logger.Warnf("database setup might be incomplete : %v", err)
		}
		desiredStreams := c.generateFabricEventStream()
		if reflect.DeepEqual(effectiveStreams.Spec, desiredStreams.Spec) {
			c.updateStreams(desiredStreams)
		}
	}

	return nil
}
