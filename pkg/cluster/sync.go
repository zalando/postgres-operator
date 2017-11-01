package cluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/volumes"
)

// Sync syncs the cluster, making sure the actual Kubernetes objects correspond to what is defined in the manifest.
// Unlike the update, sync does not error out if some objects do not exist and takes care of creating them.
func (c *Cluster) Sync(newSpec *spec.Postgresql) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Postgresql = *newSpec

	defer func() {
		if err != nil {
			c.setStatus(spec.ClusterStatusSyncFailed)
		} else if c.Status != spec.ClusterStatusRunning {
			c.setStatus(spec.ClusterStatusRunning)
		}
	}()

	if err = c.initUsers(); err != nil {
		err = fmt.Errorf("could not init users: %v", err)
		return
	}

	c.logger.Debugf("syncing secrets")

	//TODO: mind the secrets of the deleted/new users
	if err = c.syncSecrets(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			err = fmt.Errorf("could not sync secrets: %v", err)
			return
		}
	}

	c.logger.Debugf("syncing endpoints")
	if err = c.syncEndpoint(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			err = fmt.Errorf("could not sync endpoints: %v", err)
			return
		}
	}

	c.logger.Debugf("syncing services")
	for _, role := range []PostgresRole{Master, Replica} {
		if role == Replica && !c.Spec.ReplicaLoadBalancer {
			if c.Services[role] != nil {
				// delete the left over replica service
				if err = c.deleteService(role); err != nil {
					err = fmt.Errorf("could not delete obsolete %s service: %v", role, err)
					return
				}
			}
			continue
		}
		if err = c.syncService(role); err != nil {
			if !k8sutil.ResourceAlreadyExists(err) {
				err = fmt.Errorf("coud not sync %s service: %v", role, err)
				return
			}
		}
	}

	c.logger.Debugf("syncing statefulsets")
	if err = c.syncStatefulSet(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			err = fmt.Errorf("could not sync statefulsets: %v", err)
			return
		}
	}

	if !c.databaseAccessDisabled() {
		c.logger.Debugf("syncing roles")
		if err = c.syncRoles(true); err != nil {
			err = fmt.Errorf("could not sync roles: %v", err)
			return
		}
	}

	c.logger.Debugf("syncing persistent volumes")
	if err = c.syncVolumes(); err != nil {
		err = fmt.Errorf("could not sync persistent volumes: %v", err)
		return
	}

	c.logger.Debug("syncing pod disruption budgets")
	if err = c.syncPodDisruptionBudget(false); err != nil {
		err = fmt.Errorf("could not sync pod disruption budget: %v", err)
		return
	}

	return
}

func (c *Cluster) syncService(role PostgresRole) error {
	var err error

	c.setProcessName("syncing %s service", role)

	c.Services[role], err = c.KubeClient.Services(c.Namespace).Get(c.serviceName(role), metav1.GetOptions{})
	if err != nil && !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get %s service: %v", role, err)
	}

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
	match, reason := k8sutil.SameService(c.Services[role], desiredSvc)
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
	var err error
	c.Endpoint, err = c.KubeClient.Endpoints(c.Namespace).Get(c.endpointName(), metav1.GetOptions{})
	if err != nil && !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get endpoint: %v", err)
	}

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

func (c *Cluster) syncPodDisruptionBudget(isUpdate bool) error {
	var err error

	c.PodDisruptionBudget, err = c.KubeClient.PodDisruptionBudgets(c.Namespace).
		Get(c.podDisruptionBudgetName(), metav1.GetOptions{})
	if err != nil && !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get pod disruption budget: %v", err)
	}

	if c.PodDisruptionBudget == nil {
		c.logger.Infof("could not find the cluster's pod disruption budget")
		pdb, err := c.createPodDisruptionBudget()
		if err != nil {
			return fmt.Errorf("could not create pod disruption budget: %v", err)
		}
		c.logger.Infof("created missing pod disruption budget %q", util.NameFromMeta(pdb.ObjectMeta))
		return nil
	} else {
		newPDB := c.generatePodDisruptionBudget()
		if match, reason := k8sutil.SamePDB(c.PodDisruptionBudget, newPDB); !match {
			c.logPDBChanges(c.PodDisruptionBudget, newPDB, isUpdate, reason)
			if err := c.updatePodDisruptionBudget(newPDB); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Cluster) syncStatefulSet() error {
	var (
		err        error
		rollUpdate bool
	)
	c.Statefulset, err = c.KubeClient.StatefulSets(c.Namespace).Get(c.statefulSetName(), metav1.GetOptions{})

	if err != nil && !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get statefulset: %v", err)
	}

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
		desiredSS, err := c.generateStatefulSet(&c.Spec)
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

func (c *Cluster) syncSecrets() error {
	c.setProcessName("syncing secrets")
	secrets := c.generateUserSecrets()

	for secretUsername, secretSpec := range secrets {
		secret, err := c.KubeClient.Secrets(secretSpec.Namespace).Create(secretSpec)
		if k8sutil.ResourceAlreadyExists(err) {
			var userMap map[string]spec.PgUser
			curSecret, err2 := c.KubeClient.Secrets(secretSpec.Namespace).Get(secretSpec.Name, metav1.GetOptions{})
			if err2 != nil {
				return fmt.Errorf("could not get current secret: %v", err2)
			}
			c.logger.Debugf("secret %q already exists, fetching it's password", util.NameFromMeta(curSecret.ObjectMeta))
			if secretUsername == c.systemUsers[constants.SuperuserKeyName].Name {
				secretUsername = constants.SuperuserKeyName
				userMap = c.systemUsers
			} else if secretUsername == c.systemUsers[constants.ReplicationUserKeyName].Name {
				secretUsername = constants.ReplicationUserKeyName
				userMap = c.systemUsers
			} else {
				userMap = c.pgUsers
			}
			pwdUser := userMap[secretUsername]
			pwdUser.Password = string(curSecret.Data["password"])
			userMap[secretUsername] = pwdUser

			continue
		} else {
			if err != nil {
				return fmt.Errorf("could not create secret for user %q: %v", secretUsername, err)
			}
			c.Secrets[secret.UID] = secret
			c.logger.Debugf("created new secret %q, uid: %q", util.NameFromMeta(secret.ObjectMeta), secret.UID)
		}
	}

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
	c.setProcessName("syncing volumes")

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
