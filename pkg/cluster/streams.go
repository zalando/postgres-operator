package cluster

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	zalandov1alpha1 "github.com/zalando/postgres-operator/pkg/apis/zalando.org/v1alpha1"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Cluster) createStreams(appId string) {
	c.setProcessName("creating streams")

	var (
		fes *zalandov1alpha1.FabricEventStream
		err error
	)

	msg := "could not create event stream custom resource with applicationId %s: %v"

	fes = c.generateFabricEventStream(appId)
	if err != nil {
		c.logger.Warningf(msg, appId, err)
	}
	_, err = c.KubeClient.FabricEventStreams(c.Namespace).Create(context.TODO(), fes, metav1.CreateOptions{})
	if err != nil {
		c.logger.Warningf(msg, appId, err)
	}
}

func (c *Cluster) updateStreams(newEventStreams *zalandov1alpha1.FabricEventStream) error {
	c.setProcessName("updating event streams")

	_, err := c.KubeClient.FabricEventStreams(newEventStreams.Namespace).Update(context.TODO(), newEventStreams, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("could not update event stream custom resource: %v", err)
	}

	return nil
}

func (c *Cluster) deleteStreams() error {
	c.setProcessName("deleting event streams")

	// check if stream CRD is installed before trying a delete
	_, err := c.KubeClient.CustomResourceDefinitions().Get(context.TODO(), constants.EventStreamSourceCRDName, metav1.GetOptions{})
	if k8sutil.ResourceNotFound(err) {
		return nil
	}

	err = c.KubeClient.FabricEventStreams(c.Namespace).Delete(context.TODO(), c.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("could not delete event stream custom resource: %v", err)
	}

	return nil
}

func gatherApplicationIds(streams []acidv1.Stream) []string {
	appIds := make([]string, 0)
	for _, stream := range streams {
		if !util.SliceContains(appIds, stream.ApplicationId) {
			appIds = append(appIds, stream.ApplicationId)
		}
	}

	return appIds
}

func (c *Cluster) syncPostgresConfig() error {

	slots := make(map[string]map[string]string)
	desiredPatroniConfig := c.Spec.Patroni
	if len(desiredPatroniConfig.Slots) > 0 {
		slots = desiredPatroniConfig.Slots
	}

	for _, stream := range c.Spec.Streams {
		slot := map[string]string{
			"database": stream.Database,
			"plugin":   "wal2json",
			"type":     "logical",
		}
		slotName := constants.EventStreamSourceSlotPrefix + "_" + stream.Database + "_" + stream.ApplicationId
		if _, exists := slots[slotName]; !exists {
			slots[slotName] = slot
		}
	}

	if len(slots) > 0 {
		c.logger.Debugf("setting wal level to 'logical' in Postgres configuration to allow for decoding changes")
		for slotName, slot := range slots {
			c.logger.Debugf("creating logical replication slot %q in database %q", slotName, slot["database"])
		}
		desiredPatroniConfig.Slots = slots
	} else {
		return nil
	}

	// if streams are defined wal_level must be switched to logical and slots have to be defined
	desiredPgParameters := map[string]string{"wal_level": "logical"}

	pods, err := c.listPods()
	if err != nil || len(pods) == 0 {
		c.logger.Warnf("could not list pods of the statefulset: %v", err)
	}
	for i, pod := range pods {
		podName := util.NameFromMeta(pods[i].ObjectMeta)
		effectivePatroniConfig, effectivePgParameters, err := c.patroni.GetConfig(&pod)
		if err != nil {
			c.logger.Warningf("could not get Postgres config from pod %s: %v", podName, err)
			continue
		}

		_, err = c.checkAndSetGlobalPostgreSQLConfiguration(&pod, effectivePatroniConfig, desiredPatroniConfig, effectivePgParameters, desiredPgParameters)
		if err != nil {
			c.logger.Warningf("could not set PostgreSQL configuration options for pod %s: %v", podName, err)
			continue
		}
	}

	return nil
}

func (c *Cluster) generateFabricEventStream(appId string) *zalandov1alpha1.FabricEventStream {
	eventStreams := make([]zalandov1alpha1.EventStream, 0)

	for _, stream := range c.Spec.Streams {
		if stream.ApplicationId != appId {
			continue
		}
		for tableName, table := range stream.Tables {
			streamSource := c.getEventStreamSource(stream, tableName, table.IdColumn)
			streamFlow := getEventStreamFlow(stream, table.PayloadColumn)
			streamSink := getEventStreamSink(stream, table.EventType)

			eventStreams = append(eventStreams, zalandov1alpha1.EventStream{
				EventStreamFlow:   streamFlow,
				EventStreamSink:   streamSink,
				EventStreamSource: streamSource})
		}
	}

	return &zalandov1alpha1.FabricEventStream{
		TypeMeta: metav1.TypeMeta{
			Kind:       constants.EventStreamSourceCRDKind,
			APIVersion: "zalando.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        c.Name + "-" + appId,
			Namespace:   c.Namespace,
			Annotations: c.AnnotationsToPropagate(c.annotationsSet(nil)),
			// make cluster StatefulSet the owner (like with connection pooler objects)
			OwnerReferences: c.ownerReferences(),
		},
		Spec: zalandov1alpha1.FabricEventStreamSpec{
			ApplicationId: appId,
			EventStreams:  eventStreams,
		},
	}
}

func (c *Cluster) getEventStreamSource(stream acidv1.Stream, tableName, idColumn string) zalandov1alpha1.EventStreamSource {
	table, schema := getTableSchema(tableName)
	streamFilter := stream.Filter[tableName]
	return zalandov1alpha1.EventStreamSource{
		Type:             constants.EventStreamSourcePGType,
		Schema:           schema,
		EventStreamTable: getOutboxTable(table, idColumn),
		Filter:           streamFilter,
		Connection: c.getStreamConnection(
			stream.Database,
			constants.EventStreamSourceSlotPrefix+constants.UserRoleNameSuffix,
			stream.ApplicationId),
	}
}

func getEventStreamFlow(stream acidv1.Stream, payloadColumn string) zalandov1alpha1.EventStreamFlow {
	return zalandov1alpha1.EventStreamFlow{
		Type:          constants.EventStreamFlowPgGenericType,
		PayloadColumn: payloadColumn,
	}
}

func getEventStreamSink(stream acidv1.Stream, eventType string) zalandov1alpha1.EventStreamSink {
	return zalandov1alpha1.EventStreamSink{
		Type:         constants.EventStreamSinkNakadiType,
		EventType:    eventType,
		MaxBatchSize: stream.BatchSize,
	}
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

func getOutboxTable(tableName, idColumn string) zalandov1alpha1.EventStreamTable {
	return zalandov1alpha1.EventStreamTable{
		Name:     tableName,
		IDColumn: idColumn,
	}
}

func (c *Cluster) getStreamConnection(database, user, appId string) zalandov1alpha1.Connection {
	return zalandov1alpha1.Connection{
		Url:      fmt.Sprintf("jdbc:postgresql://%s.%s/%s?user=%s&ssl=true&sslmode=require", c.Name, c.Namespace, database, user),
		SlotName: constants.EventStreamSourceSlotPrefix + "_" + database + "_" + strings.Replace(appId, "-", "_", -1),
		DBAuth: zalandov1alpha1.DBAuth{
			Type:        constants.EventStreamSourceAuthType,
			Name:        c.credentialSecretNameForCluster(user, c.Name),
			UserKey:     "username",
			PasswordKey: "password",
		},
	}
}

func (c *Cluster) syncStreams() error {

	_, err := c.KubeClient.CustomResourceDefinitions().Get(context.TODO(), constants.EventStreamSourceCRDName, metav1.GetOptions{})
	if k8sutil.ResourceNotFound(err) {
		c.logger.Debugf("event stream CRD not installed, skipping")
		return nil
	}

	err = c.createOrUpdateStreams()
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) createOrUpdateStreams() error {

	c.setProcessName("syncing streams")

	err := c.syncPostgresConfig()
	if err != nil {
		return fmt.Errorf("could not update Postgres config for event streaming: %v", err)
	}

	appIds := gatherApplicationIds(c.Spec.Streams)
	for _, appId := range appIds {
		fesName := c.Name + "-" + appId
		effectiveStreams, err := c.KubeClient.FabricEventStreams(c.Namespace).Get(context.TODO(), fesName, metav1.GetOptions{})
		if err != nil {
			if !k8sutil.ResourceNotFound(err) {
				return fmt.Errorf("failed reading event stream %s: %v", fesName, err)
			}

			c.logger.Infof("event streams do not exist, create it")
			c.createStreams(appId)
		} else {
			desiredStreams := c.generateFabricEventStream(appId)
			if !reflect.DeepEqual(effectiveStreams.Spec, desiredStreams.Spec) {
				c.logger.Debug("updating event streams")
				desiredStreams.ObjectMeta.ResourceVersion = effectiveStreams.ObjectMeta.ResourceVersion
				err = c.updateStreams(desiredStreams)
				if err != nil {
					return fmt.Errorf("failed updating event stream %s: %v", fesName, err)
				}
			}
		}
	}

	return nil
}
