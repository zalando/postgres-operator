package cluster

import (
	"fmt"

	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
)

func (c *Cluster) Sync() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.loadResources()
	if err != nil {
		c.logger.Errorf("could not load resources: %v", err)
	}

	c.logger.Debugf("Syncing secrets")
	if err := c.syncSecrets(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not sync secrets: %v", err)
		}
	}

	c.logger.Debugf("Syncing endpoints")
	if err := c.syncEndpoint(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not sync endpoints: %v", err)
		}
	}

	c.logger.Debugf("Syncing services")
	if err := c.syncService(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("coud not sync services: %v", err)
		}
	}

	c.logger.Debugf("Syncing statefulsets")
	if err := c.syncStatefulSet(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not sync statefulsets: %v", err)
		}
	}

	if c.databaseAccessDisabled() {
		return nil
	}
	if err := c.initDbConn(); err != nil {
		return fmt.Errorf("could not init db connection: %v", err)
	} else {
		c.logger.Debugf("Syncing roles")
		if err := c.SyncRoles(); err != nil {
			return fmt.Errorf("could not sync roles: %v", err)
		}
	}

	return nil
}

func (c *Cluster) syncSecrets() error {
	//TODO: mind the secrets of the deleted/new users
	if err := c.initUsers(); err != nil {
		return err
	}

	err := c.applySecrets()

	return err
}

func (c *Cluster) syncService() error {
	cSpec := c.Spec
	if c.Service == nil {
		c.logger.Infof("could not find the cluster's service")
		svc, err := c.createService()
		if err != nil {
			return fmt.Errorf("could not create missing service: %v", err)
		}
		c.logger.Infof("Created missing service '%s'", util.NameFromMeta(svc.ObjectMeta))

		return nil
	}

	desiredSvc := c.genService(cSpec.AllowedSourceRanges)
	match, reason := c.sameServiceWith(desiredSvc)
	if match {
		return nil
	}
	c.logServiceChanges(c.Service, desiredSvc, false, reason)

	if err := c.updateService(desiredSvc); err != nil {
		return fmt.Errorf("could not update service to match desired state: %v", err)
	}
	c.logger.Infof("service '%s' is in the desired state now", util.NameFromMeta(desiredSvc.ObjectMeta))

	return nil
}

func (c *Cluster) syncEndpoint() error {
	if c.Endpoint == nil {
		c.logger.Infof("could not find the cluster's endpoint")
		ep, err := c.createEndpoint()
		if err != nil {
			return fmt.Errorf("could not create missing endpoint: %v", err)
		}
		c.logger.Infof("Created missing endpoint '%s'", util.NameFromMeta(ep.ObjectMeta))
		return nil
	}

	return nil
}

func (c *Cluster) syncStatefulSet() error {
	cSpec := c.Spec
	var rollUpdate, needsReplace bool
	if c.Statefulset == nil {
		c.logger.Infof("could not find the cluster's statefulset")
		pods, err := c.listPods()
		if err != nil {
			return fmt.Errorf("could not list pods of the statefulset: %v", err)
		}

		if len(pods) > 0 {
			c.logger.Infof("Found pods without the statefulset: trigger rolling update")
			rollUpdate = true
		}
		ss, err := c.createStatefulSet()
		if err != nil {
			return fmt.Errorf("could not create missing statefulset: %v", err)
		}
		err = c.waitStatefulsetPodsReady()
		if err != nil {
			return fmt.Errorf("cluster is not ready: %v", err)
		}
		c.logger.Infof("Created missing statefulset '%s'", util.NameFromMeta(ss.ObjectMeta))
		if !rollUpdate {
			return nil
		}
	}
	if !rollUpdate {
		var (
			match  bool
			reason string
		)

		desiredSS, err := c.genStatefulSet(cSpec)
		if err != nil {
			return fmt.Errorf("could not generate statefulset: %v", err)
		}

		match, needsReplace, rollUpdate, reason = c.compareStatefulSetWith(desiredSS)
		if match {
			return nil
		}
		c.logStatefulSetChanges(c.Statefulset, desiredSS, false, reason)

		if !needsReplace {
			if err := c.updateStatefulSet(desiredSS); err != nil {
				return fmt.Errorf("could not update statefulset: %v", err)
			}
		} else {
			if err := c.replaceStatefulSet(desiredSS); err != nil {
				return fmt.Errorf("could not replace statefulset: %v", err)
			}
		}

		if !rollUpdate {
			c.logger.Debugln("No rolling update is needed")
			return nil
		}
	}
	c.logger.Debugln("Performing rolling update")
	if err := c.recreatePods(); err != nil {
		return fmt.Errorf("could not recreate pods: %v", err)
	}
	c.logger.Infof("pods have been recreated")

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
		return fmt.Errorf("error getting users from the database: %v", err)
	}
	pgSyncRequests := c.userSyncStrategy.ProduceSyncRequests(dbUsers, c.pgUsers)
	if err := c.userSyncStrategy.ExecuteSyncRequests(pgSyncRequests, c.pgDb); err != nil {
		return fmt.Errorf("error executing sync statements: %v", err)
	}
	return nil
}
