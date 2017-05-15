package cluster

import (
	"fmt"

	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

func (c *Cluster) SyncCluster(stopCh <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.loadResources()

	if !c.podDispatcherRunning {
		go c.podEventsDispatcher(stopCh)
		c.podDispatcherRunning = true
	}

	c.logger.Debugf("Syncing Secrets")
	if err := c.syncSecrets(); err != nil {
		c.logger.Infof("Can't sync Secrets: %s", err)
	}

	c.logger.Debugf("Syncing Endpoints")
	if err := c.syncEndpoint(); err != nil {
		c.logger.Errorf("Can't sync Endpoints: %s", err)
	}

	c.logger.Debugf("Syncing Services")
	if err := c.syncService(); err != nil {
		c.logger.Errorf("Can't sync Services: %s", err)
	}

	c.logger.Debugf("Syncing StatefulSets")
	if err := c.syncStatefulSet(); err != nil {
		c.logger.Errorf("Can't sync StatefulSets: %s", err)
	}
	if c.DatabaseAccessDisabled() {
		return
	}
	if err := c.initDbConn(); err != nil {
		c.logger.Errorf("Can't init db connection: %s", err)
	} else {
		c.logger.Debugf("Syncing Roles")
		if err := c.SyncRoles(); err != nil {
			c.logger.Errorf("Can't sync Roles: %s", err)
		}
	}
}

func (c *Cluster) syncSecrets() error {
	//TODO: mind the secrets of the deleted/new users
	if err := c.initUsers(); err != nil {
		return err
	}

	err := c.applySecrets()
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) syncService() error {
	cSpec := c.Spec
	if c.Service == nil {
		c.logger.Infof("Can't find the cluster's Service")
		svc, err := c.createService()
		if err != nil {
			return fmt.Errorf("Can't create missing Service: %s", err)
		}
		c.logger.Infof("Created missing Service '%s'", util.NameFromMeta(svc.ObjectMeta))

		return nil
	}

	desiredSvc := c.genService(cSpec.AllowedSourceRanges)
	if match, reason := c.sameServiceWith(desiredSvc); match {
		return nil
	} else {
		c.logServiceChanges(c.Service, desiredSvc, false, reason)
	}

	if err := c.updateService(desiredSvc); err != nil {
		return fmt.Errorf("Can't update Service to match desired state: %s", err)
	}
	c.logger.Infof("Service '%s' is in the desired state now", util.NameFromMeta(desiredSvc.ObjectMeta))

	return nil
}

func (c *Cluster) syncEndpoint() error {
	if c.Endpoint == nil {
		c.logger.Infof("Can't find the cluster's Endpoint")
		ep, err := c.createEndpoint()
		if err != nil {
			return fmt.Errorf("Can't create missing Endpoint: %s", err)
		}
		c.logger.Infof("Created missing Endpoint '%s'", util.NameFromMeta(ep.ObjectMeta))
		return nil
	}

	return nil
}

func (c *Cluster) syncStatefulSet() error {
	cSpec := c.Spec
	var rollUpdate, needsReplace bool
	if c.Statefulset == nil {
		c.logger.Infof("Can't find the cluster's StatefulSet")
		pods, err := c.listPods()
		if err != nil {
			return fmt.Errorf("Can't list pods of the StatefulSet: %s", err)
		}

		if len(pods) > 0 {
			c.logger.Infof("Found pods without the statefulset: trigger rolling update")
			rollUpdate = true
		}
		ss, err := c.createStatefulSet()
		if err != nil {
			return fmt.Errorf("Can't create missing StatefulSet: %s", err)
		}
		err = c.waitStatefulsetPodsReady()
		if err != nil {
			return fmt.Errorf("Cluster is not ready: %s", err)
		}
		c.logger.Infof("Created missing StatefulSet '%s'", util.NameFromMeta(ss.ObjectMeta))
		if !rollUpdate {
			return nil
		}
	}
	if !rollUpdate {
		var (
			match  bool
			reason string
		)
		desiredSS := c.genStatefulSet(cSpec)
		match, needsReplace, rollUpdate, reason = c.compareStatefulSetWith(desiredSS)
		if match {
			return nil
		}
		c.logStatefulSetChanges(c.Statefulset, desiredSS, false, reason)

		if !needsReplace {
			if err := c.updateStatefulSet(desiredSS); err != nil {
				return fmt.Errorf("Can't update StatefulSet: %s", err)
			}
		} else {
			if err := c.replaceStatefulSet(desiredSS); err != nil {
				return fmt.Errorf("Can't replace StatefulSet: %s", err)
			}
		}

		if !rollUpdate {
			c.logger.Debugln("No rolling update is needed")
			return nil
		}
	}
	c.logger.Debugln("Performing rolling update")
	if err := c.recreatePods(); err != nil {
		return fmt.Errorf("Can't recreate Pods: %s", err)
	}
	c.logger.Infof("Pods have been recreated")

	return nil
}

func (c *Cluster) SyncRoles() error {
	var userNames []string

	if err := c.initUsers(); err != nil {
		return err
	}
	for _, u := range c.pgUsers {
		userNames = append(userNames, u.Name)
	}
	dbUsers, err := c.readPgUsersFromDatabase(userNames)
	if err != nil {
		return fmt.Errorf("Error getting users from the database: %s", err)
	}
	pgSyncRequests := c.userSyncStrategy.ProduceSyncRequests(dbUsers, c.pgUsers)
	if err := c.userSyncStrategy.ExecuteSyncRequests(pgSyncRequests, c.pgDb); err != nil {
		return fmt.Errorf("Error executing sync statements: %s", err)
	}
	return nil
}
