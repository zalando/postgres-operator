package cluster

import (
	"context"
	"encoding/json"
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
	"k8s.io/apimachinery/pkg/types"
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
	patch, err := json.Marshal(newEventStreams)
	if err != nil {
		return fmt.Errorf("could not marshal new event stream CRD %q: %v", newEventStreams.Name, err)
	}
	if _, err := c.KubeClient.FabricEventStreams(newEventStreams.Namespace).Patch(
		context.TODO(), newEventStreams.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) deleteStream(stream *zalandov1.FabricEventStream) error {
	c.setProcessName("deleting event stream")

	err := c.KubeClient.FabricEventStreams(stream.Namespace).Delete(context.TODO(), stream.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("could not delete event stream %q: %v", stream.Name, err)
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
		err := c.deleteStream(&stream)
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

func (c *Cluster) syncPublication(dbName string, databaseSlotsList map[string]zalandov1.Slot, slotsToSync *map[string]map[string]string) error {
	createPublications := make(map[string]string)
	alterPublications := make(map[string]string)
	deletePublications := []string{}

	defer func() {
		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection: %v", err)
		}
	}()

	// check for existing publications
	if err := c.initDbConnWithName(dbName); err != nil {
		return fmt.Errorf("could not init database connection: %v", err)
	}

	currentPublications, err := c.getPublications()
	if err != nil {
		return fmt.Errorf("could not get current publications: %v", err)
	}

	for slotName, slotAndPublication := range databaseSlotsList {
		tables := slotAndPublication.Publication
		tableNames := make([]string, len(tables))
		i := 0
		for t := range tables {
			tableName, schemaName := getTableSchema(t)
			tableNames[i] = fmt.Sprintf("%s.%s", schemaName, tableName)
			i++
		}
		sort.Strings(tableNames)
		tableList := strings.Join(tableNames, ", ")

		currentTables, exists := currentPublications[slotName]
		if !exists {
			createPublications[slotName] = tableList
		} else if currentTables != tableList {
			alterPublications[slotName] = tableList
		}
	}

	// check if there is any deletion
	for slotName, _ := range currentPublications {
		if _, exists := databaseSlotsList[slotName]; !exists {
			deletePublications = append(deletePublications, slotName)
		}
	}

	if len(createPublications)+len(alterPublications)+len(deletePublications) == 0 {
		return nil
	}

	var errorMessage error = nil
	for publicationName, tables := range createPublications {
		if err = c.executeCreatePublication(publicationName, tables); err != nil {
			errorMessage = fmt.Errorf("creation of publication %q failed: %v", publicationName, err)
			continue
		}
		(*slotsToSync)[publicationName] = databaseSlotsList[publicationName].Slot
	}
	for publicationName, tables := range alterPublications {
		if err = c.executeAlterPublication(publicationName, tables); err != nil {
			errorMessage = fmt.Errorf("update of publication %q failed: %v", publicationName, err)
			continue
		}
		(*slotsToSync)[publicationName] = databaseSlotsList[publicationName].Slot
	}
	for _, publicationName := range deletePublications {
		if err = c.executeDropPublication(publicationName); err != nil {
			errorMessage = fmt.Errorf("deletion of publication %q failed: %v", publicationName, err)
			continue
		}
		(*slotsToSync)[publicationName] = nil
	}

	return errorMessage
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
			streamRecovery := getEventStreamRecovery(stream, table.RecoveryEventType, table.EventType)

			eventStreams = append(eventStreams, zalandov1.EventStream{
				EventStreamFlow:     streamFlow,
				EventStreamRecovery: streamRecovery,
				EventStreamSink:     streamSink,
				EventStreamSource:   streamSource})
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

func getEventStreamRecovery(stream acidv1.Stream, recoveryEventType, eventType string) zalandov1.EventStreamRecovery {
	if (stream.EnableRecovery != nil && !*stream.EnableRecovery) ||
		(stream.EnableRecovery == nil && recoveryEventType == "") {
		return zalandov1.EventStreamRecovery{
			Type: constants.EventStreamRecoveryNoneType,
		}
	}

	if stream.EnableRecovery != nil && *stream.EnableRecovery && recoveryEventType == "" {
		recoveryEventType = fmt.Sprintf("%s-%s", eventType, constants.EventStreamRecoverySuffix)
	}

	return zalandov1.EventStreamRecovery{
		Type: constants.EventStreamRecoveryDLQType,
		Sink: &zalandov1.EventStreamSink{
			Type:         constants.EventStreamSinkNakadiType,
			EventType:    recoveryEventType,
			MaxBatchSize: stream.BatchSize,
		},
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

	databaseSlots := make(map[string]map[string]zalandov1.Slot)
	slotsToSync := make(map[string]map[string]string)
	requiredPatroniConfig := c.Spec.Patroni

	if len(requiredPatroniConfig.Slots) > 0 {
		for slotName, slotConfig := range requiredPatroniConfig.Slots {
			slotsToSync[slotName] = slotConfig
		}
	}

	if err := c.initDbConn(); err != nil {
		return fmt.Errorf("could not init database connection")
	}
	defer func() {
		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection: %v", err)
		}
	}()
	listDatabases, err := c.getDatabases()
	if err != nil {
		return fmt.Errorf("could not get list of databases: %v", err)
	}
	// get database name with empty list of slot, except template0 and template1
	for dbName, _ := range listDatabases {
		if dbName != "template0" && dbName != "template1" {
			databaseSlots[dbName] = map[string]zalandov1.Slot{}
		}
	}

	// gather list of required slots and publications, group by database
	for _, stream := range c.Spec.Streams {
		if _, exists := databaseSlots[stream.Database]; !exists {
			c.logger.Warningf("database %q does not exist in the cluster", stream.Database)
			continue
		}
		slot := map[string]string{
			"database": stream.Database,
			"plugin":   constants.EventStreamSourcePluginType,
			"type":     "logical",
		}
		slotName := getSlotName(stream.Database, stream.ApplicationId)
		if _, exists := databaseSlots[stream.Database][slotName]; !exists {
			databaseSlots[stream.Database][slotName] = zalandov1.Slot{
				Slot:        slot,
				Publication: stream.Tables,
			}
		} else {
			slotAndPublication := databaseSlots[stream.Database][slotName]
			streamTables := slotAndPublication.Publication
			for tableName, table := range stream.Tables {
				if _, exists := streamTables[tableName]; !exists {
					streamTables[tableName] = table
				}
			}
			slotAndPublication.Publication = streamTables
			databaseSlots[stream.Database][slotName] = slotAndPublication
		}
	}

	// sync publication in a database
	c.logger.Debug("syncing database publications")
	for dbName, databaseSlotsList := range databaseSlots {
		err := c.syncPublication(dbName, databaseSlotsList, &slotsToSync)
		if err != nil {
			c.logger.Warningf("could not sync publications in database %q: %v", dbName, err)
			continue
		}
	}

	c.logger.Debug("syncing logical replication slots")
	pods, err := c.listPods()
	if err != nil {
		return fmt.Errorf("could not get list of pods to sync logical replication slots via Patroni API: %v", err)
	}

	// sync logical replication slots in Patroni config
	requiredPatroniConfig.Slots = slotsToSync
	configPatched, _, _, err := c.syncPatroniConfig(pods, requiredPatroniConfig, nil)
	if err != nil {
		c.logger.Warningf("Patroni config updated? %v - errors during config sync: %v", configPatched, err)
	}

	// finally sync stream CRDs
	err = c.createOrUpdateStreams(slotsToSync)
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) createOrUpdateStreams(createdSlots map[string]map[string]string) error {

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

	for idx, appId := range appIds {
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
			// check if there is any slot with the applicationId
			slotName := getSlotName(c.Spec.Streams[idx].Database, appId)
			if _, exists := createdSlots[slotName]; !exists {
				c.logger.Warningf("no slot %s with applicationId %s exists, skipping event stream creation", slotName, appId)
				continue
			}
			c.logger.Infof("event streams with applicationId %s do not exist, create it", appId)
			streamCRD, err := c.createStreams(appId)
			if err != nil {
				return fmt.Errorf("failed creating event streams with applicationId %s: %v", appId, err)
			}
			c.logger.Infof("event streams %q have been successfully created", streamCRD.Name)
		}
	}

	// check if there is any deletion
	for _, stream := range streams.Items {
		if !util.SliceContains(appIds, stream.Spec.ApplicationId) {
			c.logger.Infof("event streams with applicationId %s do not exist in the manifest, delete it", stream.Spec.ApplicationId)
			err := c.deleteStream(&stream)
			if err != nil {
				return fmt.Errorf("failed deleting event streams with applicationId %s: %v", stream.Spec.ApplicationId, err)
			}
			c.logger.Infof("event streams %q have been successfully deleted", stream.Name)
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
				reflect.DeepEqual(newStream.EventStreamSink, curStream.EventStreamSink) &&
				reflect.DeepEqual(newStream.EventStreamRecovery, curStream.EventStreamRecovery) {
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
