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

func (c *Cluster) createStreams(appId string) (*zalandov1.FabricEventStream, error) {
	c.setProcessName("creating streams")

	fes := c.generateFabricEventStream(appId)
	streamCRD, err := c.KubeClient.FabricEventStreams(c.Namespace).Create(context.TODO(), fes, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return streamCRD, nil
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

	errors := make([]string, 0)
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelsSet(true).String(),
	}
	streams, err := c.KubeClient.FabricEventStreams(c.Namespace).List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("could not list of FabricEventStreams: %v", err)
	}
	for _, stream := range streams.Items {
		err = c.KubeClient.FabricEventStreams(stream.Namespace).Delete(context.TODO(), stream.Name, metav1.DeleteOptions{})
		if err != nil {
			errors = append(errors, fmt.Sprintf("could not delete event stream %q: %v", stream.Name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("could not delete all event stream custom resources: %v", strings.Join(errors, `', '`))
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

func (c *Cluster) syncPostgresConfig(requiredPatroniConfig acidv1.Patroni) (bool, error) {
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

		configPatched, _, err := c.checkAndSetGlobalPostgreSQLConfiguration(&pod, effectivePatroniConfig, requiredPatroniConfig, effectivePgParameters, requiredPgParameters)
		if err != nil {
			errorMsg = fmt.Sprintf("could not set PostgreSQL configuration options for pod %s: %v", podName, err)
			continue
		}

		// Patroni's config endpoint is just a "proxy" to DCS. It is enough to patch it only once and it doesn't matter which pod is used
		return configPatched, nil
	}

	return false, fmt.Errorf(errorMsg)
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
			// max length for cluster name is 58 so we can only add 5 more characters / numbers
			Name:        fmt.Sprintf("%s-%s", c.Name, strings.ToLower(util.RandomPassword(5))),
			Namespace:   c.Namespace,
			Labels:      c.labelsSet(true),
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

func (c *Cluster) getEventStreamSource(stream acidv1.Stream, tableName string, idColumn *string) zalandov1.EventStreamSource {
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

func getEventStreamFlow(stream acidv1.Stream, payloadColumn *string) zalandov1.EventStreamFlow {
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

func getOutboxTable(tableName string, idColumn *string) zalandov1.EventStreamTable {
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

	// update config to set wal_level: logical
	requiredPatroniConfig := c.Spec.Patroni
	requiresRestart, err := c.syncPostgresConfig(requiredPatroniConfig)
	if err != nil {
		return fmt.Errorf("failed to snyc Postgres config for event streaming: %v", err)
	}
	if requiresRestart {
		c.logger.Debugf("updated Postgres config. Server will be restarted and streams will get created during next sync")
		return nil
	}

	slots := make(map[string]map[string]string)
	slotsToSync := make(map[string]map[string]string)
	publications := make(map[string]map[string]acidv1.StreamTable)

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

	// create publications to each created slot
	c.logger.Debug("syncing database publications")
	for publication, tables := range publications {
		// but first check for existing publications
		dbName := slots[publication]["database"]
		err = c.syncPublication(publication, dbName, tables)
		if err != nil {
			c.logger.Warningf("could not sync publication %q in database %q: %v", publication, dbName, err)
			continue
		}
		slotsToSync[publication] = slots[publication]
	}

	// no slots to sync = no streams defined or publications created
	if len(slotsToSync) > 0 {
		requiredPatroniConfig.Slots = slotsToSync
	} else {
		return nil
	}

	// add extra logical slots to Patroni config
	_, err = c.syncPostgresConfig(requiredPatroniConfig)
	if err != nil {
		return fmt.Errorf("failed to snyc Postgres config for event streaming: %v", err)
	}

	// after Postgres was restarted we can create stream CRDs
	err = c.createOrUpdateStreams()
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) createOrUpdateStreams() error {

	// fetch different application IDs from streams section
	// there will be a separate event stream resource for each ID
	appIds := gatherApplicationIds(c.Spec.Streams)

	// list all existing stream CRDs
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelsSet(true).String(),
	}
	streams, err := c.KubeClient.FabricEventStreams(c.Namespace).List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("could not list of FabricEventStreams: %v", err)
	}

	for _, appId := range appIds {
		streamExists := false

		// update stream when it exists and EventStreams array differs
		for _, stream := range streams.Items {
			if appId == stream.Spec.ApplicationId {
				streamExists = true
				desiredStreams := c.generateFabricEventStream(appId)
				if match, reason := sameStreams(stream.Spec.EventStreams, desiredStreams.Spec.EventStreams); !match {
					c.logger.Debugf("updating event streams: %s", reason)
					desiredStreams.ObjectMeta = stream.ObjectMeta
					err = c.updateStreams(desiredStreams)
					if err != nil {
						return fmt.Errorf("failed updating event stream %s: %v", stream.Name, err)
					}
					c.logger.Infof("event stream %q has been successfully updated", stream.Name)
				}
				continue
			}
		}

		if !streamExists {
			c.logger.Infof("event streams with applicationId %s do not exist, create it", appId)
			streamCRD, err := c.createStreams(appId)
			if err != nil {
				return fmt.Errorf("failed creating event streams with applicationId %s: %v", appId, err)
			}
			c.logger.Infof("event streams %q have been successfully created", streamCRD.Name)
		}
	}

	return nil
}

func sameStreams(curEventStreams, newEventStreams []zalandov1.EventStream) (match bool, reason string) {
	if len(newEventStreams) != len(curEventStreams) {
		return false, "number of defined streams is different"
	}

	for _, newStream := range newEventStreams {
		match = false
		reason = "event stream specs differ"
		for _, curStream := range curEventStreams {
			if reflect.DeepEqual(newStream.EventStreamSource, curStream.EventStreamSource) &&
				reflect.DeepEqual(newStream.EventStreamFlow, curStream.EventStreamFlow) &&
				reflect.DeepEqual(newStream.EventStreamSink, curStream.EventStreamSink) {
				match = true
				break
			}
		}
		if !match {
			return false, reason
		}
	}

	return true, ""
}
