package cluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	zalandov1 "github.com/zalando/postgres-operator/pkg/apis/zalando.org/v1"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Cluster) createStreams(appId string) error {
	c.setProcessName("creating streams")

	fes := c.generateFabricEventStream(appId)
	if _, err := c.KubeClient.FabricEventStreams(c.Namespace).Create(context.TODO(), fes, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) updateStreams(newEventStreams *zalandov1.FabricEventStream) error {
	c.setProcessName("updating event streams")

	if _, err := c.KubeClient.FabricEventStreams(newEventStreams.Namespace).Update(context.TODO(), newEventStreams, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) deleteStreams() error {
	c.setProcessName("deleting event streams")

	// check if stream CRD is installed before trying a delete
	_, err := c.KubeClient.CustomResourceDefinitions().Get(context.TODO(), constants.EventStreamCRDName, metav1.GetOptions{})
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

func (c *Cluster) syncPostgresConfig(requiredPatroniConfig acidv1.Patroni) error {
	errorMsg := "no pods found to update config"

	// if streams are defined wal_level must be switched to logical
	requiredPgParameters := map[string]string{"wal_level": "logical"}

	// apply config changes in pods
	pods, err := c.listPods()
	if err != nil {
		errorMsg = fmt.Sprintf("could not list pods of the statefulset: %v", err)
	}
	for i, pod := range pods {
		podName := util.NameFromMeta(pods[i].ObjectMeta)
		effectivePatroniConfig, effectivePgParameters, err := c.patroni.GetConfig(&pod)
		if err != nil {
			errorMsg = fmt.Sprintf("could not get Postgres config from pod %s: %v", podName, err)
			continue
		}

		_, err = c.checkAndSetGlobalPostgreSQLConfiguration(&pod, effectivePatroniConfig, requiredPatroniConfig, effectivePgParameters, requiredPgParameters)
		if err != nil {
			errorMsg = fmt.Sprintf("could not set PostgreSQL configuration options for pod %s: %v", podName, err)
			continue
		}

		// Patroni's config endpoint is just a "proxy" to DCS. It is enough to patch it only once and it doesn't matter which pod is used
		return nil
	}

	return fmt.Errorf(errorMsg)
}

func (c *Cluster) syncPublication(publication, dbName string, tables map[string]acidv1.StreamTable) error {
	createPublications := make(map[string]string)
	alterPublications := make(map[string]string)

	defer func() {
		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection: %v", err)
		}
	}()

	// check for existing publications
	if err := c.initDbConnWithName(dbName); err != nil {
		return fmt.Errorf("could not init database connection")
	}

	currentPublications, err := c.getPublications()
	if err != nil {
		return fmt.Errorf("could not get current publications: %v", err)
	}

	tableNames := make([]string, len(tables))
	i := 0
	for t := range tables {
		tableName, schemaName := getTableSchema(t)
		tableNames[i] = fmt.Sprintf("%s.%s", schemaName, tableName)
		i++
	}
	sort.Strings(tableNames)
	tableList := strings.Join(tableNames, ", ")

	currentTables, exists := currentPublications[publication]
	if !exists {
		createPublications[publication] = tableList
	} else if currentTables != tableList {
		alterPublications[publication] = tableList
	}

	if len(createPublications)+len(alterPublications) == 0 {
		return nil
	}

	for publicationName, tables := range createPublications {
		if err = c.executeCreatePublication(publicationName, tables); err != nil {
			return fmt.Errorf("creation of publication %q failed: %v", publicationName, err)
		}
	}
	for publicationName, tables := range alterPublications {
		if err = c.executeAlterPublication(publicationName, tables); err != nil {
			return fmt.Errorf("update of publication %q failed: %v", publicationName, err)
		}
	}

	return nil
}

func (c *Cluster) generateFabricEventStream(appId string) *zalandov1.FabricEventStream {
	eventStreams := make([]zalandov1.EventStream, 0)

	for _, stream := range c.Spec.Streams {
		if stream.ApplicationId != appId {
			continue
		}
		for tableName, table := range stream.Tables {
			streamSource := c.getEventStreamSource(stream, tableName, table.IdColumn)
			streamFlow := getEventStreamFlow(stream, table.PayloadColumn)
			streamSink := getEventStreamSink(stream, table.EventType)

			eventStreams = append(eventStreams, zalandov1.EventStream{
				EventStreamFlow:   streamFlow,
				EventStreamSink:   streamSink,
				EventStreamSource: streamSource})
		}
	}

	return &zalandov1.FabricEventStream{
		TypeMeta: metav1.TypeMeta{
			APIVersion: constants.EventStreamCRDApiVersion,
			Kind:       constants.EventStreamCRDKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-%s", c.Name, appId),
			Namespace:   c.Namespace,
			Annotations: c.AnnotationsToPropagate(c.annotationsSet(nil)),
			// make cluster StatefulSet the owner (like with connection pooler objects)
			OwnerReferences: c.ownerReferences(),
		},
		Spec: zalandov1.FabricEventStreamSpec{
			ApplicationId: appId,
			EventStreams:  eventStreams,
		},
	}
}

func (c *Cluster) getEventStreamSource(stream acidv1.Stream, tableName, idColumn string) zalandov1.EventStreamSource {
	table, schema := getTableSchema(tableName)
	streamFilter := stream.Filter[tableName]
	return zalandov1.EventStreamSource{
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

func getEventStreamFlow(stream acidv1.Stream, payloadColumn string) zalandov1.EventStreamFlow {
	return zalandov1.EventStreamFlow{
		Type:          constants.EventStreamFlowPgGenericType,
		PayloadColumn: payloadColumn,
	}
}

func getEventStreamSink(stream acidv1.Stream, eventType string) zalandov1.EventStreamSink {
	return zalandov1.EventStreamSink{
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

func getOutboxTable(tableName, idColumn string) zalandov1.EventStreamTable {
	return zalandov1.EventStreamTable{
		Name:     tableName,
		IDColumn: idColumn,
	}
}

func getSlotName(dbName, appId string) string {
	return fmt.Sprintf("%s_%s_%s", constants.EventStreamSourceSlotPrefix, dbName, strings.Replace(appId, "-", "_", -1))
}

func (c *Cluster) getStreamConnection(database, user, appId string) zalandov1.Connection {
	return zalandov1.Connection{
		Url:        fmt.Sprintf("jdbc:postgresql://%s.%s/%s?user=%s&ssl=true&sslmode=require", c.Name, c.Namespace, database, user),
		SlotName:   getSlotName(database, appId),
		PluginType: constants.EventStreamSourcePluginType,
		DBAuth: zalandov1.DBAuth{
			Type:        constants.EventStreamSourceAuthType,
			Name:        c.credentialSecretNameForCluster(user, c.Name),
			UserKey:     "username",
			PasswordKey: "password",
		},
	}
}

func (c *Cluster) syncStreams() error {

	c.setProcessName("syncing streams")

	_, err := c.KubeClient.CustomResourceDefinitions().Get(context.TODO(), constants.EventStreamCRDName, metav1.GetOptions{})
	if k8sutil.ResourceNotFound(err) {
		c.logger.Debugf("event stream CRD not installed, skipping")
		return nil
	}

	slots := make(map[string]map[string]string)
	publications := make(map[string]map[string]acidv1.StreamTable)

	requiredPatroniConfig := c.Spec.Patroni
	if len(requiredPatroniConfig.Slots) > 0 {
		slots = requiredPatroniConfig.Slots
	}

	// gather list of required slots and publications
	for _, stream := range c.Spec.Streams {
		slot := map[string]string{
			"database": stream.Database,
			"plugin":   constants.EventStreamSourcePluginType,
			"type":     "logical",
		}
		slotName := getSlotName(stream.Database, stream.ApplicationId)
		if _, exists := slots[slotName]; !exists {
			slots[slotName] = slot
			publications[slotName] = stream.Tables
		} else {
			streamTables := publications[slotName]
			for tableName, table := range stream.Tables {
				if _, exists := streamTables[tableName]; !exists {
					streamTables[tableName] = table
				}
			}
			publications[slotName] = streamTables
		}
	}

	// no slots = no streams defined
	if len(slots) > 0 {
		requiredPatroniConfig.Slots = slots
	} else {
		return nil
	}

	// add extra logical slots to Patroni config
	c.logger.Debug("syncing Postgres config for logical decoding")
	err = c.syncPostgresConfig(requiredPatroniConfig)
	if err != nil {
		return fmt.Errorf("failed to snyc Postgres config for event streaming: %v", err)
	}

	// next, create publications to each created slot
	c.logger.Debug("syncing database publications")
	for publication, tables := range publications {
		// but first check for existing publications
		dbName := slots[publication]["database"]
		err = c.syncPublication(publication, dbName, tables)
		if err != nil {
			c.logger.Warningf("could not sync publication %q in database %q: %v", publication, dbName, err)
		}
	}

	err = c.createOrUpdateStreams()
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) createOrUpdateStreams() error {

	appIds := gatherApplicationIds(c.Spec.Streams)
	for _, appId := range appIds {
		fesName := fmt.Sprintf("%s-%s", c.Name, appId)
		effectiveStreams, err := c.KubeClient.FabricEventStreams(c.Namespace).Get(context.TODO(), fesName, metav1.GetOptions{})
		if err != nil {
			if !k8sutil.ResourceNotFound(err) {
				return fmt.Errorf("failed reading event stream %s: %v", fesName, err)
			}

			c.logger.Infof("event streams do not exist, create it")
			err = c.createStreams(appId)
			if err != nil {
				return fmt.Errorf("failed creating event stream %s: %v", fesName, err)
			}
			c.logger.Infof("event stream %q has been successfully created", fesName)
		} else {
			desiredStreams := c.generateFabricEventStream(appId)
			if !reflect.DeepEqual(effectiveStreams.Spec, desiredStreams.Spec) {
				c.logger.Debug("updating event streams")
				desiredStreams.ObjectMeta.ResourceVersion = effectiveStreams.ObjectMeta.ResourceVersion
				err = c.updateStreams(desiredStreams)
				if err != nil {
					return fmt.Errorf("failed updating event stream %s: %v", fesName, err)
				}
				c.logger.Infof("event stream %q has been successfully updated", fesName)
			}
		}
	}

	return nil
}
