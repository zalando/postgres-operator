package cluster

import (
	"fmt"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/volumes"
)

// Sync syncs the cluster, making sure the actual Kubernetes objects correspond to what is defined in the manifest.
// Unlike the update, sync does not error out if some objects do not exist and takes care of creating them.
func (c *Cluster) Sync() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.loadResources()
	if err != nil {
		c.logger.Errorf("could not load resources: %v", err)
	}

	if err = c.initUsers(); err != nil {
		return err
	}

	c.logger.Debugf("syncing secrets")

	//TODO: mind the secrets of the deleted/new users
	if err := c.applySecrets(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not sync secrets: %v", err)
		}
	}

	c.logger.Debugf("syncing endpoints")
	if err := c.syncEndpoint(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not sync endpoints: %v", err)
		}
	}

	c.logger.Debugf("syncing services")
	for _, role := range []PostgresRole{Master, Replica} {
		if role == Replica && !c.Spec.ReplicaLoadBalancer {
			if c.Services[role] != nil {
				// delete the left over replica service
				if err := c.deleteService(role); err != nil {
					return fmt.Errorf("could not delete obsolete %s service: %v", role, err)
				}
			}
			continue
		}
		if err := c.syncService(role); err != nil {
			if !k8sutil.ResourceAlreadyExists(err) {
				return fmt.Errorf("coud not sync %s service: %v", role, err)
			}
		}
	}

	c.logger.Debugf("syncing statefulsets")
	if err := c.syncStatefulSet(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not sync statefulsets: %v", err)
		}
	}

	if !c.databaseAccessDisabled() {
		c.logger.Debugf("syncing roles")
		if err := c.syncRoles(true); err != nil {
			return fmt.Errorf("could not sync roles: %v", err)
		}
	}

	c.logger.Debugf("syncing persistent volumes")
	if err := c.syncVolumes(); err != nil {
		return fmt.Errorf("could not sync persistent volumes: %v", err)
	}

	return nil
}

func (c *Cluster) syncService(role PostgresRole) error {
	cSpec := c.Spec
	if c.Services[role] == nil {
		c.logger.Infof("could not find the cluster's %s service", role)
		svc, err := c.createService(role)
		if err != nil {
			return fmt.Errorf("could not create missing %s service: %v", role, err)
		}
		c.logger.Infof("created missing %s service %q", role, util.NameFromMeta(svc.ObjectMeta))

		return nil
	}

	desiredSvc := c.generateService(role, &cSpec)
	match, reason := c.sameServiceWith(role, desiredSvc)
	if match {
		return nil
	}
	c.logServiceChanges(role, c.Services[role], desiredSvc, false, reason)

	if err := c.updateService(role, desiredSvc); err != nil {
		return fmt.Errorf("could not update %s service to match desired state: %v", role, err)
	}
	c.logger.Infof("%s service %q is in the desired state now", role, util.NameFromMeta(desiredSvc.ObjectMeta))

	return nil
}

func (c *Cluster) syncEndpoint() error {
	if c.Endpoint == nil {
		c.logger.Infof("could not find the cluster's endpoint")
		ep, err := c.createEndpoint()
		if err != nil {
			return fmt.Errorf("could not create missing endpoint: %v", err)
		}
		c.logger.Infof("created missing endpoint %q", util.NameFromMeta(ep.ObjectMeta))
		return nil
	}

	return nil
}

func (c *Cluster) syncStatefulSet() error {
	cSpec := c.Spec
	var rollUpdate bool
	if c.Statefulset == nil {
		c.logger.Infof("could not find the cluster's statefulset")
		pods, err := c.listPods()
		if err != nil {
			return fmt.Errorf("could not list pods of the statefulset: %v", err)
		}

		if len(pods) > 0 {
			c.logger.Infof("found pods without the statefulset: trigger rolling update")
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
		c.logger.Infof("created missing statefulset %q", util.NameFromMeta(ss.ObjectMeta))
		if !rollUpdate {
			return nil
		}
	}
	/* TODO: should check that we need to replace the statefulset */
	if !rollUpdate {
		desiredSS, err := c.generateStatefulSet(cSpec)
		if err != nil {
			return fmt.Errorf("could not generate statefulset: %v", err)
		}

		cmp := c.compareStatefulSetWith(desiredSS)
		if cmp.match {
			return nil
		}
		c.logStatefulSetChanges(c.Statefulset, desiredSS, false, cmp.reasons)

		if !cmp.replace {
			if err := c.updateStatefulSet(desiredSS); err != nil {
				return fmt.Errorf("could not update statefulset: %v", err)
			}
		} else {
			if err := c.replaceStatefulSet(desiredSS); err != nil {
				return fmt.Errorf("could not replace statefulset: %v", err)
			}
		}

		if !cmp.rollingUpdate {
			c.logger.Debugln("no rolling update is needed")
			return nil
		}
	}
	c.logger.Debugln("performing rolling update")
	if err := c.recreatePods(); err != nil {
		return fmt.Errorf("could not recreate pods: %v", err)
	}
	c.logger.Infof("pods have been recreated")

	return nil
}

func (c *Cluster) syncRoles(readFromDatabase bool) error {
	c.setProcessName("syncing roles")

	var (
		err       error
		dbUsers   spec.PgUserMap
		userNames []string
	)

	err = c.initDbConn()
	if err != nil {
		return fmt.Errorf("could not init db connection: %v", err)
	}
	defer c.closeDbConn()

	if readFromDatabase {
		for _, u := range c.pgUsers {
			userNames = append(userNames, u.Name)
		}
		dbUsers, err = c.readPgUsersFromDatabase(userNames)
		if err != nil {
			return fmt.Errorf("error getting users from the database: %v", err)
		}
	}

	pgSyncRequests := c.userSyncStrategy.ProduceSyncRequests(dbUsers, c.pgUsers)
	if err = c.userSyncStrategy.ExecuteSyncRequests(pgSyncRequests, c.pgDb); err != nil {
		return fmt.Errorf("error executing sync statements: %v", err)
	}
	return nil
}

// syncVolumes reads all persistent volumes and checks that their size matches the one declared in the statefulset.
func (c *Cluster) syncVolumes() error {
	act, err := c.volumesNeedResizing(c.Spec.Volume)
	if err != nil {
		return fmt.Errorf("could not compare size of the volumes: %v", err)
	}
	if !act {
		return nil
	}
	if err := c.resizeVolumes(c.Spec.Volume, []volumes.VolumeResizer{&volumes.EBSVolumeResizer{}}); err != nil {
		return fmt.Errorf("could not sync volumes: %v", err)
	}
	c.logger.Infof("volumes have been synced successfully")
	return nil
}
