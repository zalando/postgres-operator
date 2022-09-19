package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	policybeta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var requirePrimaryRestartWhenDecreased = []string{
	"max_connections",
	"max_prepared_transactions",
	"max_locks_per_transaction",
	"max_worker_processes",
	"max_wal_senders",
}

// Sync syncs the cluster, making sure the actual Kubernetes objects correspond to what is defined in the manifest.
// Unlike the update, sync does not error out if some objects do not exist and takes care of creating them.
func (c *Cluster) Sync(newSpec *acidv1.Postgresql) error {
	var err error
	c.mu.Lock()
	defer c.mu.Unlock()

	oldSpec := c.Postgresql
	c.setSpec(newSpec)

	defer func() {
		if err != nil {
			c.logger.Warningf("error while syncing cluster state: %v", err)
			c.KubeClient.SetPostgresCRDStatus(c.clusterName(), acidv1.ClusterStatusSyncFailed)
		} else if !c.Status.Running() {
			c.KubeClient.SetPostgresCRDStatus(c.clusterName(), acidv1.ClusterStatusRunning)
		}
	}()

	if err = c.initUsers(); err != nil {
		err = fmt.Errorf("could not init users: %v", err)
		return err
	}

	//TODO: mind the secrets of the deleted/new users
	if err = c.syncSecrets(); err != nil {
		err = fmt.Errorf("could not sync secrets: %v", err)
		return err
	}

	if err = c.syncServices(); err != nil {
		err = fmt.Errorf("could not sync services: %v", err)
		return err
	}

	// sync volume may already transition volumes to gp3, if iops/throughput or type is specified
	if err = c.syncVolumes(); err != nil {
		return err
	}

	if c.OpConfig.EnableEBSGp3Migration && len(c.EBSVolumes) > 0 {
		err = c.executeEBSMigration()
		if nil != err {
			return err
		}
	}

	c.logger.Debug("syncing statefulsets")
	if err = c.syncStatefulSet(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			err = fmt.Errorf("could not sync statefulsets: %v", err)
			return err
		}
	}

	c.logger.Debug("syncing pod disruption budgets")
	if err = c.syncPodDisruptionBudget(false); err != nil {
		err = fmt.Errorf("could not sync pod disruption budget: %v", err)
		return err
	}

	// create a logical backup job unless we are running without pods or disable that feature explicitly
	if c.Spec.EnableLogicalBackup && c.getNumberOfInstances(&c.Spec) > 0 {

		c.logger.Debug("syncing logical backup job")
		if err = c.syncLogicalBackupJob(); err != nil {
			err = fmt.Errorf("could not sync the logical backup job: %v", err)
			return err
		}
	}

	// create database objects unless we are running without pods or disabled that feature explicitly
	if !(c.databaseAccessDisabled() || c.getNumberOfInstances(&newSpec.Spec) <= 0 || c.Spec.StandbyCluster != nil) {
		c.logger.Debug("syncing roles")
		if err = c.syncRoles(); err != nil {
			err = fmt.Errorf("could not sync roles: %v", err)
			return err
		}
		c.logger.Debug("syncing databases")
		if err = c.syncDatabases(); err != nil {
			err = fmt.Errorf("could not sync databases: %v", err)
			return err
		}
		c.logger.Debug("syncing prepared databases with schemas")
		if err = c.syncPreparedDatabases(); err != nil {
			err = fmt.Errorf("could not sync prepared database: %v", err)
			return err
		}
	}

	// sync connection pooler
	if _, err = c.syncConnectionPooler(&oldSpec, newSpec, c.installLookupFunction); err != nil {
		return fmt.Errorf("could not sync connection pooler: %v", err)
	}

	if len(c.Spec.Streams) > 0 {
		c.logger.Debug("syncing streams")
		if err = c.syncStreams(); err != nil {
			err = fmt.Errorf("could not sync streams: %v", err)
			return err
		}
	}

	// Major version upgrade must only run after success of all earlier operations, must remain last item in sync
	if err := c.majorVersionUpgrade(); err != nil {
		c.logger.Errorf("major version upgrade failed: %v", err)
	}

	return err
}

func (c *Cluster) syncServices() error {
	for _, role := range []PostgresRole{Master, Replica} {
		c.logger.Debugf("syncing %s service", role)

		if !c.patroniKubernetesUseConfigMaps() {
			if err := c.syncEndpoint(role); err != nil {
				return fmt.Errorf("could not sync %s endpoint: %v", role, err)
			}
		}
		if err := c.syncService(role); err != nil {
			return fmt.Errorf("could not sync %s service: %v", role, err)
		}
	}

	return nil
}

func (c *Cluster) syncService(role PostgresRole) error {
	var (
		svc *v1.Service
		err error
	)
	c.setProcessName("syncing %s service", role)

	if svc, err = c.KubeClient.Services(c.Namespace).Get(context.TODO(), c.serviceName(role), metav1.GetOptions{}); err == nil {
		c.Services[role] = svc
		desiredSvc := c.generateService(role, &c.Spec)
		if match, reason := c.compareServices(svc, desiredSvc); !match {
			c.logServiceChanges(role, svc, desiredSvc, false, reason)
			updatedSvc, err := c.updateService(role, svc, desiredSvc)
			if err != nil {
				return fmt.Errorf("could not update %s service to match desired state: %v", role, err)
			}
			c.Services[role] = updatedSvc
			c.logger.Infof("%s service %q is in the desired state now", role, util.NameFromMeta(desiredSvc.ObjectMeta))
		}
		return nil
	}
	if !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get %s service: %v", role, err)
	}
	// no existing service, create new one
	c.Services[role] = nil
	c.logger.Infof("could not find the cluster's %s service", role)

	if svc, err = c.createService(role); err == nil {
		c.logger.Infof("created missing %s service %q", role, util.NameFromMeta(svc.ObjectMeta))
	} else {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not create missing %s service: %v", role, err)
		}
		c.logger.Infof("%s service %q already exists", role, util.NameFromMeta(svc.ObjectMeta))
		if svc, err = c.KubeClient.Services(c.Namespace).Get(context.TODO(), c.serviceName(role), metav1.GetOptions{}); err != nil {
			return fmt.Errorf("could not fetch existing %s service: %v", role, err)
		}
	}
	c.Services[role] = svc
	return nil
}

func (c *Cluster) syncEndpoint(role PostgresRole) error {
	var (
		ep  *v1.Endpoints
		err error
	)
	c.setProcessName("syncing %s endpoint", role)

	if ep, err = c.KubeClient.Endpoints(c.Namespace).Get(context.TODO(), c.endpointName(role), metav1.GetOptions{}); err == nil {
		// TODO: No syncing of endpoints here, is this covered completely by updateService?
		c.Endpoints[role] = ep
		return nil
	}
	if !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get %s endpoint: %v", role, err)
	}
	// no existing endpoint, create new one
	c.Endpoints[role] = nil
	c.logger.Infof("could not find the cluster's %s endpoint", role)

	if ep, err = c.createEndpoint(role); err == nil {
		c.logger.Infof("created missing %s endpoint %q", role, util.NameFromMeta(ep.ObjectMeta))
	} else {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not create missing %s endpoint: %v", role, err)
		}
		c.logger.Infof("%s endpoint %q already exists", role, util.NameFromMeta(ep.ObjectMeta))
		if ep, err = c.KubeClient.Endpoints(c.Namespace).Get(context.TODO(), c.endpointName(role), metav1.GetOptions{}); err != nil {
			return fmt.Errorf("could not fetch existing %s endpoint: %v", role, err)
		}
	}
	c.Endpoints[role] = ep
	return nil
}

func (c *Cluster) syncPodDisruptionBudget(isUpdate bool) error {
	var (
		pdb *policybeta1.PodDisruptionBudget
		err error
	)
	if pdb, err = c.KubeClient.PodDisruptionBudgets(c.Namespace).Get(context.TODO(), c.podDisruptionBudgetName(), metav1.GetOptions{}); err == nil {
		c.PodDisruptionBudget = pdb
		newPDB := c.generatePodDisruptionBudget()
		if match, reason := k8sutil.SamePDB(pdb, newPDB); !match {
			c.logPDBChanges(pdb, newPDB, isUpdate, reason)
			if err = c.updatePodDisruptionBudget(newPDB); err != nil {
				return err
			}
		} else {
			c.PodDisruptionBudget = pdb
		}
		return nil

	}
	if !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get pod disruption budget: %v", err)
	}
	// no existing pod disruption budget, create new one
	c.PodDisruptionBudget = nil
	c.logger.Infof("could not find the cluster's pod disruption budget")

	if pdb, err = c.createPodDisruptionBudget(); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not create pod disruption budget: %v", err)
		}
		c.logger.Infof("pod disruption budget %q already exists", util.NameFromMeta(pdb.ObjectMeta))
		if pdb, err = c.KubeClient.PodDisruptionBudgets(c.Namespace).Get(context.TODO(), c.podDisruptionBudgetName(), metav1.GetOptions{}); err != nil {
			return fmt.Errorf("could not fetch existing %q pod disruption budget", util.NameFromMeta(pdb.ObjectMeta))
		}
	}

	c.logger.Infof("created missing pod disruption budget %q", util.NameFromMeta(pdb.ObjectMeta))
	c.PodDisruptionBudget = pdb

	return nil
}

func (c *Cluster) syncStatefulSet() error {
	var (
		restartWait         uint32
		configPatched       bool
		restartPrimaryFirst bool
	)
	podsToRecreate := make([]v1.Pod, 0)
	isSafeToRecreatePods := true
	switchoverCandidates := make([]spec.NamespacedName, 0)

	pods, err := c.listPods()
	if err != nil {
		c.logger.Warnf("could not list pods of the statefulset: %v", err)
	}

	// NB: Be careful to consider the codepath that acts on podsRollingUpdateRequired before returning early.
	sset, err := c.KubeClient.StatefulSets(c.Namespace).Get(context.TODO(), c.statefulSetName(), metav1.GetOptions{})
	if err != nil {
		if !k8sutil.ResourceNotFound(err) {
			return fmt.Errorf("error during reading of statefulset: %v", err)
		}
		// statefulset does not exist, try to re-create it
		c.Statefulset = nil
		c.logger.Infof("cluster's statefulset does not exist")

		sset, err = c.createStatefulSet()
		if err != nil {
			return fmt.Errorf("could not create missing statefulset: %v", err)
		}

		if err = c.waitStatefulsetPodsReady(); err != nil {
			return fmt.Errorf("cluster is not ready: %v", err)
		}

		if len(pods) > 0 {
			for _, pod := range pods {
				if err = c.markRollingUpdateFlagForPod(&pod, "pod from previous statefulset"); err != nil {
					c.logger.Warnf("marking old pod for rolling update failed: %v", err)
				}
				podsToRecreate = append(podsToRecreate, pod)
			}
		}
		c.logger.Infof("created missing statefulset %q", util.NameFromMeta(sset.ObjectMeta))

	} else {
		// check if there are still pods with a rolling update flag
		for _, pod := range pods {
			if c.getRollingUpdateFlagFromPod(&pod) {
				podsToRecreate = append(podsToRecreate, pod)
			} else {
				role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])
				if role == Master {
					continue
				}
				switchoverCandidates = append(switchoverCandidates, util.NameFromMeta(pod.ObjectMeta))
			}
		}

		if len(podsToRecreate) > 0 {
			c.logger.Debugf("%d / %d pod(s) still need to be rotated", len(podsToRecreate), len(pods))
		}

		// statefulset is already there, make sure we use its definition in order to compare with the spec.
		c.Statefulset = sset

		desiredSts, err := c.generateStatefulSet(&c.Spec)
		if err != nil {
			return fmt.Errorf("could not generate statefulset: %v", err)
		}

		cmp := c.compareStatefulSetWith(desiredSts)
		if !cmp.match {
			if cmp.rollingUpdate {
				podsToRecreate = make([]v1.Pod, 0)
				switchoverCandidates = make([]spec.NamespacedName, 0)
				for _, pod := range pods {
					if err = c.markRollingUpdateFlagForPod(&pod, "pod changes"); err != nil {
						return fmt.Errorf("updating rolling update flag for pod failed: %v", err)
					}
					podsToRecreate = append(podsToRecreate, pod)
				}
			}

			c.logStatefulSetChanges(c.Statefulset, desiredSts, false, cmp.reasons)

			if !cmp.replace {
				if err := c.updateStatefulSet(desiredSts); err != nil {
					return fmt.Errorf("could not update statefulset: %v", err)
				}
			} else {
				if err := c.replaceStatefulSet(desiredSts); err != nil {
					return fmt.Errorf("could not replace statefulset: %v", err)
				}
			}
		}

		if len(podsToRecreate) == 0 && !c.OpConfig.EnableLazySpiloUpgrade {
			// even if the desired and the running statefulsets match
			// there still may be not up-to-date pods on condition
			//  (a) the lazy update was just disabled
			// and
			//  (b) some of the pods were not restarted when the lazy update was still in place
			for _, pod := range pods {
				effectivePodImage := getPostgresContainer(&pod.Spec).Image
				stsImage := getPostgresContainer(&desiredSts.Spec.Template.Spec).Image

				if stsImage != effectivePodImage {
					if err = c.markRollingUpdateFlagForPod(&pod, "pod not yet restarted due to lazy update"); err != nil {
						c.logger.Warnf("updating rolling update flag failed for pod %q: %v", pod.Name, err)
					}
					podsToRecreate = append(podsToRecreate, pod)
				} else {
					role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])
					if role == Master {
						continue
					}
					switchoverCandidates = append(switchoverCandidates, util.NameFromMeta(pod.ObjectMeta))
				}
			}
		}
	}

	// Apply special PostgreSQL parameters that can only be set via the Patroni API.
	// it is important to do it after the statefulset pods are there, but before the rolling update
	// since those parameters require PostgreSQL restart.
	pods, err = c.listPods()
	if err != nil {
		c.logger.Warnf("could not get list of pods to apply special PostgreSQL parameters only to be set via Patroni API: %v", err)
	}

	// get Postgres config, compare with manifest and update via Patroni PATCH endpoint if it differs
	// Patroni's config endpoint is just a "proxy" to DCS. It is enough to patch it only once and it doesn't matter which pod is used
	for i, pod := range pods {
		patroniConfig, pgParameters, err := c.getPatroniConfig(&pod)
		if err != nil {
			c.logger.Warningf("%v", err)
			isSafeToRecreatePods = false
			continue
		}
		restartWait = patroniConfig.LoopWait

		// empty config probably means cluster is not fully initialized yet, e.g. restoring from backup
		// do not attempt a restart
		if !reflect.DeepEqual(patroniConfig, acidv1.Patroni{}) || len(pgParameters) > 0 {
			// compare config returned from Patroni with what is specified in the manifest
			configPatched, restartPrimaryFirst, err = c.checkAndSetGlobalPostgreSQLConfiguration(&pod, patroniConfig, c.Spec.Patroni, pgParameters, c.Spec.Parameters)
			if err != nil {
				c.logger.Warningf("could not set PostgreSQL configuration options for pod %s: %v", pods[i].Name, err)
				continue
			}

			// it could take up to LoopWait to apply the config
			if configPatched {
				time.Sleep(time.Duration(restartWait)*time.Second + time.Second*2)
				break
			}
		}
	}

	// restart instances if it is still pending
	remainingPods := make([]*v1.Pod, 0)
	skipRole := Master
	if restartPrimaryFirst {
		skipRole = Replica
	}
	for i, pod := range pods {
		role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])
		if role == skipRole {
			remainingPods = append(remainingPods, &pods[i])
			continue
		}
		if err = c.restartInstance(&pod, restartWait); err != nil {
			c.logger.Errorf("%v", err)
			isSafeToRecreatePods = false
		}
	}

	// in most cases only the master should be left to restart
	if len(remainingPods) > 0 {
		for _, remainingPod := range remainingPods {
			if err = c.restartInstance(remainingPod, restartWait); err != nil {
				c.logger.Errorf("%v", err)
				isSafeToRecreatePods = false
			}
		}
	}

	// if we get here we also need to re-create the pods (either leftovers from the old
	// statefulset or those that got their configuration from the outdated statefulset)
	if len(podsToRecreate) > 0 {
		if isSafeToRecreatePods {
			c.logger.Debugln("performing rolling update")
			c.eventRecorder.Event(c.GetReference(), v1.EventTypeNormal, "Update", "Performing rolling update")
			if err := c.recreatePods(podsToRecreate, switchoverCandidates); err != nil {
				return fmt.Errorf("could not recreate pods: %v", err)
			}
			c.eventRecorder.Event(c.GetReference(), v1.EventTypeNormal, "Update", "Rolling update done - pods have been recreated")
		} else {
			c.logger.Warningf("postpone pod recreation until next sync")
		}
	}

	return nil
}

func (c *Cluster) restartInstance(pod *v1.Pod, restartWait uint32) error {
	// if the config update requires a restart, call Patroni restart
	podName := util.NameFromMeta(pod.ObjectMeta)
	role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])
	memberData, err := c.getPatroniMemberData(pod)
	if err != nil {
		return fmt.Errorf("could not restart Postgres in %s pod %s: %v", role, podName, err)
	}

	// do restart only when it is pending
	if memberData.PendingRestart {
		c.eventRecorder.Event(c.GetReference(), v1.EventTypeNormal, "Update", fmt.Sprintf("restarting Postgres server within %s pod %s", role, podName))
		if err := c.patroni.Restart(pod); err != nil {
			return err
		}
		time.Sleep(time.Duration(restartWait) * time.Second)
		c.eventRecorder.Event(c.GetReference(), v1.EventTypeNormal, "Update", fmt.Sprintf("Postgres server restart done for %s pod %s", role, podName))
	}

	return nil
}

// AnnotationsToPropagate get the annotations to update if required
// based on the annotations in postgres CRD
func (c *Cluster) AnnotationsToPropagate(annotations map[string]string) map[string]string {

	if annotations == nil {
		annotations = make(map[string]string)
	}

	pgCRDAnnotations := c.ObjectMeta.Annotations

	if pgCRDAnnotations != nil {
		for _, anno := range c.OpConfig.DownscalerAnnotations {
			for k, v := range pgCRDAnnotations {
				matched, err := regexp.MatchString(anno, k)
				if err != nil {
					c.logger.Errorf("annotations matching issue: %v", err)
					return nil
				}
				if matched {
					annotations[k] = v
				}
			}
		}
	}

	if len(annotations) > 0 {
		return annotations
	}

	return nil
}

// checkAndSetGlobalPostgreSQLConfiguration checks whether cluster-wide API parameters
// (like max_connections) have changed and if necessary sets it via the Patroni API
func (c *Cluster) checkAndSetGlobalPostgreSQLConfiguration(pod *v1.Pod, effectivePatroniConfig, desiredPatroniConfig acidv1.Patroni, effectivePgParameters, desiredPgParameters map[string]string) (bool, bool, error) {
	configToSet := make(map[string]interface{})
	parametersToSet := make(map[string]string)
	restartPrimary := make([]bool, 0)
	configPatched := false
	requiresMasterRestart := false

	// compare effective and desired Patroni config options
	if desiredPatroniConfig.LoopWait > 0 && desiredPatroniConfig.LoopWait != effectivePatroniConfig.LoopWait {
		configToSet["loop_wait"] = desiredPatroniConfig.LoopWait
	}
	if desiredPatroniConfig.MaximumLagOnFailover > 0 && desiredPatroniConfig.MaximumLagOnFailover != effectivePatroniConfig.MaximumLagOnFailover {
		configToSet["maximum_lag_on_failover"] = desiredPatroniConfig.MaximumLagOnFailover
	}
	if desiredPatroniConfig.PgHba != nil && !reflect.DeepEqual(desiredPatroniConfig.PgHba, effectivePatroniConfig.PgHba) {
		configToSet["pg_hba"] = desiredPatroniConfig.PgHba
	}
	if desiredPatroniConfig.RetryTimeout > 0 && desiredPatroniConfig.RetryTimeout != effectivePatroniConfig.RetryTimeout {
		configToSet["retry_timeout"] = desiredPatroniConfig.RetryTimeout
	}
	if desiredPatroniConfig.SynchronousMode != effectivePatroniConfig.SynchronousMode {
		configToSet["synchronous_mode"] = desiredPatroniConfig.SynchronousMode
	}
	if desiredPatroniConfig.SynchronousModeStrict != effectivePatroniConfig.SynchronousModeStrict {
		configToSet["synchronous_mode_strict"] = desiredPatroniConfig.SynchronousModeStrict
	}
	if desiredPatroniConfig.TTL > 0 && desiredPatroniConfig.TTL != effectivePatroniConfig.TTL {
		configToSet["ttl"] = desiredPatroniConfig.TTL
	}

	// check if specified slots exist in config and if they differ
	slotsToSet := make(map[string]map[string]string)
	for slotName, desiredSlot := range desiredPatroniConfig.Slots {
		if effectiveSlot, exists := effectivePatroniConfig.Slots[slotName]; exists {
			if reflect.DeepEqual(desiredSlot, effectiveSlot) {
				continue
			}
		}
		slotsToSet[slotName] = desiredSlot
	}
	if len(slotsToSet) > 0 {
		configToSet["slots"] = slotsToSet
	}

	// compare effective and desired parameters under postgresql section in Patroni config
	for desiredOption, desiredValue := range desiredPgParameters {
		effectiveValue := effectivePgParameters[desiredOption]
		if isBootstrapOnlyParameter(desiredOption) && (effectiveValue != desiredValue) {
			parametersToSet[desiredOption] = desiredValue
			if util.SliceContains(requirePrimaryRestartWhenDecreased, desiredOption) {
				effectiveValueNum, errConv := strconv.Atoi(effectiveValue)
				desiredValueNum, errConv2 := strconv.Atoi(desiredValue)
				if errConv != nil || errConv2 != nil {
					continue
				}
				if effectiveValueNum > desiredValueNum {
					restartPrimary = append(restartPrimary, true)
					continue
				}
			}
			restartPrimary = append(restartPrimary, false)
		}
	}

	// check if there exist only config updates that require a restart of the primary
	if !util.SliceContains(restartPrimary, false) && len(configToSet) == 0 {
		requiresMasterRestart = true
	}

	if len(parametersToSet) > 0 {
		configToSet["postgresql"] = map[string]interface{}{constants.PatroniPGParametersParameterName: parametersToSet}
	}

	if len(configToSet) == 0 {
		return configPatched, requiresMasterRestart, nil
	}

	configToSetJson, err := json.Marshal(configToSet)
	if err != nil {
		c.logger.Debugf("could not convert config patch to JSON: %v", err)
	}

	// try all pods until the first one that is successful, as it doesn't matter which pod
	// carries the request to change configuration through
	podName := util.NameFromMeta(pod.ObjectMeta)
	c.logger.Debugf("patching Postgres config via Patroni API on pod %s with following options: %s",
		podName, configToSetJson)
	if err = c.patroni.SetConfig(pod, configToSet); err != nil {
		return configPatched, requiresMasterRestart, fmt.Errorf("could not patch postgres parameters within pod %s: %v", podName, err)
	}
	configPatched = true

	return configPatched, requiresMasterRestart, nil
}

func (c *Cluster) syncSecrets() error {

	c.logger.Info("syncing secrets")
	c.setProcessName("syncing secrets")
	generatedSecrets := c.generateUserSecrets()
	rotationUsers := make(spec.PgUserMap)
	retentionUsers := make([]string, 0)
	currentTime := time.Now()

	for secretUsername, generatedSecret := range generatedSecrets {
		secret, err := c.KubeClient.Secrets(generatedSecret.Namespace).Create(context.TODO(), generatedSecret, metav1.CreateOptions{})
		if err == nil {
			c.Secrets[secret.UID] = secret
			c.logger.Debugf("created new secret %s, namespace: %s, uid: %s", util.NameFromMeta(secret.ObjectMeta), generatedSecret.Namespace, secret.UID)
			continue
		}
		if k8sutil.ResourceAlreadyExists(err) {
			if err = c.updateSecret(secretUsername, generatedSecret, &rotationUsers, &retentionUsers, currentTime); err != nil {
				c.logger.Warningf("syncing secret %s failed: %v", util.NameFromMeta(secret.ObjectMeta), err)
			}
		} else {
			return fmt.Errorf("could not create secret for user %s: in namespace %s: %v", secretUsername, generatedSecret.Namespace, err)
		}
	}

	// add new user with date suffix and use it in the secret of the original user
	if len(rotationUsers) > 0 {
		err := c.initDbConn()
		if err != nil {
			return fmt.Errorf("could not init db connection: %v", err)
		}
		pgSyncRequests := c.userSyncStrategy.ProduceSyncRequests(spec.PgUserMap{}, rotationUsers)
		if err = c.userSyncStrategy.ExecuteSyncRequests(pgSyncRequests, c.pgDb); err != nil {
			return fmt.Errorf("error creating database roles for password rotation: %v", err)
		}
		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection after creating users for password rotation: %v", err)
		}
	}

	// remove rotation users that exceed the retention interval
	if len(retentionUsers) > 0 {
		err := c.initDbConn()
		if err != nil {
			return fmt.Errorf("could not init db connection: %v", err)
		}
		if err = c.cleanupRotatedUsers(retentionUsers, c.pgDb); err != nil {
			return fmt.Errorf("error removing users exceeding configured retention interval: %v", err)
		}
		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection after removing users exceeding configured retention interval: %v", err)
		}
	}

	return nil
}

func (c *Cluster) getNextRotationDate(currentDate time.Time) (time.Time, string) {
	nextRotationDate := currentDate.AddDate(0, 0, int(c.OpConfig.PasswordRotationInterval))
	return nextRotationDate, nextRotationDate.Format(time.RFC3339)
}

func (c *Cluster) updateSecret(
	secretUsername string,
	generatedSecret *v1.Secret,
	rotationUsers *spec.PgUserMap,
	retentionUsers *[]string,
	currentTime time.Time) error {
	var (
		secret          *v1.Secret
		err             error
		updateSecret    bool
		updateSecretMsg string
	)

	// get the secret first
	if secret, err = c.KubeClient.Secrets(generatedSecret.Namespace).Get(context.TODO(), generatedSecret.Name, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("could not get current secret: %v", err)
	}
	c.Secrets[secret.UID] = secret

	// fetch user map to update later
	var userMap map[string]spec.PgUser
	var userKey string
	if secretUsername == c.systemUsers[constants.SuperuserKeyName].Name {
		userKey = constants.SuperuserKeyName
		userMap = c.systemUsers
	} else if secretUsername == c.systemUsers[constants.ReplicationUserKeyName].Name {
		userKey = constants.ReplicationUserKeyName
		userMap = c.systemUsers
	} else {
		userKey = secretUsername
		userMap = c.pgUsers
	}

	// use system user when pooler is enabled and pooler user is specfied in manifest
	if _, exists := c.systemUsers[constants.ConnectionPoolerUserKeyName]; exists {
		if secretUsername == c.systemUsers[constants.ConnectionPoolerUserKeyName].Name {
			userKey = constants.ConnectionPoolerUserKeyName
			userMap = c.systemUsers
		}
	}
	// use system user when streams are defined and fes_user is specfied in manifest
	if _, exists := c.systemUsers[constants.EventStreamUserKeyName]; exists {
		if secretUsername == c.systemUsers[constants.EventStreamUserKeyName].Name {
			userKey = constants.EventStreamUserKeyName
			userMap = c.systemUsers
		}
	}

	pwdUser := userMap[userKey]
	secretName := util.NameFromMeta(secret.ObjectMeta)

	// if password rotation is enabled update password and username if rotation interval has been passed
	// rotation can be enabled globally or via the manifest (excluding the Postgres superuser)
	rotationEnabledInManifest := secretUsername != constants.SuperuserKeyName &&
		(util.SliceContains(c.Spec.UsersWithSecretRotation, secretUsername) ||
			util.SliceContains(c.Spec.UsersWithInPlaceSecretRotation, secretUsername))

	// globally enabled rotation is only allowed for manifest and bootstrapped roles
	allowedRoleTypes := []spec.RoleOrigin{spec.RoleOriginManifest, spec.RoleOriginBootstrap}
	rotationAllowed := !pwdUser.IsDbOwner && util.SliceContains(allowedRoleTypes, pwdUser.Origin)

	if (c.OpConfig.EnablePasswordRotation && rotationAllowed) || rotationEnabledInManifest {
		updateSecretMsg, err = c.rotatePasswordInSecret(secret, pwdUser, secretUsername, currentTime, rotationUsers, retentionUsers)
		if err != nil {
			c.logger.Warnf("password rotation failed for user %s: %v", secretUsername, err)
		}
		if updateSecretMsg != "" {
			updateSecret = true
		}
	} else {
		// username might not match if password rotation has been disabled again
		if secretUsername != string(secret.Data["username"]) {
			*retentionUsers = append(*retentionUsers, secretUsername)
			secret.Data["username"] = []byte(secretUsername)
			secret.Data["password"] = []byte(util.RandomPassword(constants.PasswordLength))
			secret.Data["nextRotation"] = []byte{}
			updateSecret = true
			updateSecretMsg = fmt.Sprintf("secret %s does not contain the role %s - updating username and resetting password", secretName, secretUsername)
		}
	}

	// if this secret belongs to the infrastructure role and the password has changed - replace it in the secret
	if pwdUser.Password != string(secret.Data["password"]) && pwdUser.Origin == spec.RoleOriginInfrastructure {
		secret = generatedSecret
		updateSecret = true
		updateSecretMsg = fmt.Sprintf("updating the secret %s from the infrastructure roles", secretName)
	} else {
		// for non-infrastructure role - update the role with the password from the secret
		pwdUser.Password = string(secret.Data["password"])
		userMap[userKey] = pwdUser
	}

	if updateSecret {
		c.logger.Debugln(updateSecretMsg)
		if _, err = c.KubeClient.Secrets(secret.Namespace).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("could not update secret %s: %v", secretName, err)
		}
		c.Secrets[secret.UID] = secret
	}

	return nil
}

func (c *Cluster) rotatePasswordInSecret(
	secret *v1.Secret,
	secretPgUser spec.PgUser,
	secretUsername string,
	currentTime time.Time,
	rotationUsers *spec.PgUserMap,
	retentionUsers *[]string) (string, error) {
	var (
		err                 error
		nextRotationDate    time.Time
		nextRotationDateStr string
		updateSecretMsg     string
	)

	secretName := util.NameFromMeta(secret.ObjectMeta)

	// initialize password rotation setting first rotation date
	nextRotationDateStr = string(secret.Data["nextRotation"])
	if nextRotationDate, err = time.ParseInLocation(time.RFC3339, nextRotationDateStr, currentTime.UTC().Location()); err != nil {
		nextRotationDate, nextRotationDateStr = c.getNextRotationDate(currentTime)
		secret.Data["nextRotation"] = []byte(nextRotationDateStr)
		updateSecretMsg = fmt.Sprintf("rotation date not found in secret %s. Setting it to %s", secretName, nextRotationDateStr)
	}

	// check if next rotation can happen sooner
	// if rotation interval has been decreased
	currentRotationDate, nextRotationDateStr := c.getNextRotationDate(currentTime)
	if nextRotationDate.After(currentRotationDate) {
		nextRotationDate = currentRotationDate
	}

	// update password and next rotation date if configured interval has passed
	if currentTime.After(nextRotationDate) {
		// create rotation user if role is not listed for in-place password update
		if !util.SliceContains(c.Spec.UsersWithInPlaceSecretRotation, secretUsername) {
			rotationUser := secretPgUser
			newRotationUsername := fmt.Sprintf("%s%s", secretUsername, currentTime.Format("060102"))
			rotationUser.Name = newRotationUsername
			rotationUser.MemberOf = []string{secretUsername}
			(*rotationUsers)[newRotationUsername] = rotationUser
			secret.Data["username"] = []byte(newRotationUsername)

			// whenever there is a rotation, check if old rotation users can be deleted
			*retentionUsers = append(*retentionUsers, secretUsername)
		} else {
			// when passwords of system users are rotated in place, pods have to be replaced
			if secretPgUser.Origin == spec.RoleOriginSystem {
				pods, err := c.listPods()
				if err != nil {
					return "", fmt.Errorf("could not list pods of the statefulset: %v", err)
				}
				for _, pod := range pods {
					if err = c.markRollingUpdateFlagForPod(&pod,
						fmt.Sprintf("replace pod due to password rotation of system user %s", secretUsername)); err != nil {
						c.logger.Warnf("marking pod for rolling update due to password rotation failed: %v", err)
					}
				}
			}

			// when password of connection pooler is rotated in place, pooler pods have to be replaced
			if secretPgUser.Origin == spec.RoleOriginConnectionPooler {
				listOptions := metav1.ListOptions{
					LabelSelector: c.poolerLabelsSet(true).String(),
				}
				poolerPods, err := c.listPoolerPods(listOptions)
				if err != nil {
					return "", fmt.Errorf("could not list pods of the pooler deployment: %v", err)
				}
				for _, poolerPod := range poolerPods {
					if err = c.markRollingUpdateFlagForPod(&poolerPod,
						fmt.Sprintf("replace pooler pod due to password rotation of pooler user %s", secretUsername)); err != nil {
						c.logger.Warnf("marking pooler pod for rolling update due to password rotation failed: %v", err)
					}
				}
			}

			// when password of stream user is rotated in place, it should trigger rolling update in FES deployment
			if secretPgUser.Origin == spec.RoleOriginStream {
				c.logger.Warnf("secret of stream user %s changed", constants.EventStreamSourceSlotPrefix+constants.UserRoleNameSuffix)
			}
		}
		secret.Data["password"] = []byte(util.RandomPassword(constants.PasswordLength))
		secret.Data["nextRotation"] = []byte(nextRotationDateStr)
		updateSecretMsg = fmt.Sprintf("updating secret %s due to password rotation - next rotation date: %s", secretName, nextRotationDateStr)
	}

	return updateSecretMsg, nil
}

func (c *Cluster) syncRoles() (err error) {
	c.setProcessName("syncing roles")

	var (
		dbUsers   spec.PgUserMap
		newUsers  spec.PgUserMap
		userNames []string
	)

	err = c.initDbConn()
	if err != nil {
		return fmt.Errorf("could not init db connection: %v", err)
	}

	defer func() {
		if err2 := c.closeDbConn(); err2 != nil {
			if err == nil {
				err = fmt.Errorf("could not close database connection: %v", err2)
			} else {
				err = fmt.Errorf("could not close database connection: %v (prior error: %v)", err2, err)
			}
		}
	}()

	// mapping between original role name and with deletion suffix
	deletedUsers := map[string]string{}
	newUsers = make(map[string]spec.PgUser)

	// create list of database roles to query
	for _, u := range c.pgUsers {
		pgRole := u.Name
		userNames = append(userNames, pgRole)
		// add team member role name with rename suffix in case we need to rename it back
		if u.Origin == spec.RoleOriginTeamsAPI && c.OpConfig.EnableTeamMemberDeprecation {
			deletedUsers[pgRole+c.OpConfig.RoleDeletionSuffix] = pgRole
			userNames = append(userNames, pgRole+c.OpConfig.RoleDeletionSuffix)
		}
	}

	// add team members that exist only in cache
	// to trigger a rename of the role in ProduceSyncRequests
	for _, cachedUser := range c.pgUsersCache {
		if _, exists := c.pgUsers[cachedUser.Name]; !exists {
			userNames = append(userNames, cachedUser.Name)
		}
	}

	// copy map for ProduceSyncRequests to include also system users
	for userName, pgUser := range c.pgUsers {
		newUsers[userName] = pgUser
	}
	for _, systemUser := range c.systemUsers {
		userNames = append(userNames, systemUser.Name)
		newUsers[systemUser.Name] = systemUser
	}

	dbUsers, err = c.readPgUsersFromDatabase(userNames)
	if err != nil {
		return fmt.Errorf("error getting users from the database: %v", err)
	}

	// update pgUsers where a deleted role was found
	// so that they are skipped in ProduceSyncRequests
	for _, dbUser := range dbUsers {
		if originalUser, exists := deletedUsers[dbUser.Name]; exists {
			recreatedUser := c.pgUsers[originalUser]
			recreatedUser.Deleted = true
			c.pgUsers[originalUser] = recreatedUser
		}
	}

	pgSyncRequests := c.userSyncStrategy.ProduceSyncRequests(dbUsers, newUsers)
	if err = c.userSyncStrategy.ExecuteSyncRequests(pgSyncRequests, c.pgDb); err != nil {
		return fmt.Errorf("error executing sync statements: %v", err)
	}

	return nil
}

func (c *Cluster) syncDatabases() error {
	c.setProcessName("syncing databases")

	createDatabases := make(map[string]string)
	alterOwnerDatabases := make(map[string]string)
	preparedDatabases := make([]string, 0)

	if err := c.initDbConn(); err != nil {
		return fmt.Errorf("could not init database connection")
	}
	defer func() {
		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection: %v", err)
		}
	}()

	currentDatabases, err := c.getDatabases()
	if err != nil {
		return fmt.Errorf("could not get current databases: %v", err)
	}

	// if no prepared databases are specified create a database named like the cluster
	if c.Spec.PreparedDatabases != nil && len(c.Spec.PreparedDatabases) == 0 { // TODO: add option to disable creating such a default DB
		c.Spec.PreparedDatabases = map[string]acidv1.PreparedDatabase{strings.Replace(c.Name, "-", "_", -1): {}}
	}
	for preparedDatabaseName := range c.Spec.PreparedDatabases {
		_, exists := currentDatabases[preparedDatabaseName]
		if !exists {
			createDatabases[preparedDatabaseName] = fmt.Sprintf("%s%s", preparedDatabaseName, constants.OwnerRoleNameSuffix)
			preparedDatabases = append(preparedDatabases, preparedDatabaseName)
		}
	}

	for databaseName, newOwner := range c.Spec.Databases {
		currentOwner, exists := currentDatabases[databaseName]
		if !exists {
			createDatabases[databaseName] = newOwner
		} else if currentOwner != newOwner {
			alterOwnerDatabases[databaseName] = newOwner
		}
	}

	if len(createDatabases)+len(alterOwnerDatabases) == 0 {
		return nil
	}

	for databaseName, owner := range createDatabases {
		if err = c.executeCreateDatabase(databaseName, owner); err != nil {
			return err
		}
	}
	for databaseName, owner := range alterOwnerDatabases {
		if err = c.executeAlterDatabaseOwner(databaseName, owner); err != nil {
			return err
		}
	}

	if len(createDatabases) > 0 {
		// trigger creation of pooler objects in new database in syncConnectionPooler
		if c.ConnectionPooler != nil {
			for _, role := range [2]PostgresRole{Master, Replica} {
				c.ConnectionPooler[role].LookupFunction = false
			}
		}
	}

	// set default privileges for prepared database
	for _, preparedDatabase := range preparedDatabases {
		if err := c.initDbConnWithName(preparedDatabase); err != nil {
			return fmt.Errorf("could not init database connection to %s", preparedDatabase)
		}

		for _, owner := range c.getOwnerRoles(preparedDatabase, c.Spec.PreparedDatabases[preparedDatabase].DefaultUsers) {
			if err = c.execAlterGlobalDefaultPrivileges(owner, preparedDatabase); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Cluster) syncPreparedDatabases() error {
	c.setProcessName("syncing prepared databases")
	for preparedDbName, preparedDB := range c.Spec.PreparedDatabases {
		if err := c.initDbConnWithName(preparedDbName); err != nil {
			return fmt.Errorf("could not init connection to database %s: %v", preparedDbName, err)
		}

		c.logger.Debugf("syncing prepared database %q", preparedDbName)
		// now, prepare defined schemas
		preparedSchemas := preparedDB.PreparedSchemas
		if len(preparedDB.PreparedSchemas) == 0 {
			preparedSchemas = map[string]acidv1.PreparedSchema{"data": {DefaultRoles: util.True()}}
		}
		if err := c.syncPreparedSchemas(preparedDbName, preparedSchemas); err != nil {
			return err
		}

		// install extensions
		if err := c.syncExtensions(preparedDB.Extensions); err != nil {
			return err
		}

		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection: %v", err)
		}
	}

	return nil
}

func (c *Cluster) syncPreparedSchemas(databaseName string, preparedSchemas map[string]acidv1.PreparedSchema) error {
	c.setProcessName("syncing prepared schemas")

	currentSchemas, err := c.getSchemas()
	if err != nil {
		return fmt.Errorf("could not get current schemas: %v", err)
	}

	var schemas []string

	for schema := range preparedSchemas {
		schemas = append(schemas, schema)
	}

	if createPreparedSchemas, equal := util.SubstractStringSlices(schemas, currentSchemas); !equal {
		for _, schemaName := range createPreparedSchemas {
			owner := constants.OwnerRoleNameSuffix
			dbOwner := fmt.Sprintf("%s%s", databaseName, owner)
			if preparedSchemas[schemaName].DefaultRoles == nil || *preparedSchemas[schemaName].DefaultRoles {
				owner = fmt.Sprintf("%s_%s%s", databaseName, schemaName, owner)
			} else {
				owner = dbOwner
			}
			if err = c.executeCreateDatabaseSchema(databaseName, schemaName, dbOwner, owner); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Cluster) syncExtensions(extensions map[string]string) error {
	c.setProcessName("syncing database extensions")

	createExtensions := make(map[string]string)
	alterExtensions := make(map[string]string)

	currentExtensions, err := c.getExtensions()
	if err != nil {
		return fmt.Errorf("could not get current database extensions: %v", err)
	}

	for extName, newSchema := range extensions {
		currentSchema, exists := currentExtensions[extName]
		if !exists {
			createExtensions[extName] = newSchema
		} else if currentSchema != newSchema {
			alterExtensions[extName] = newSchema
		}
	}

	for extName, schema := range createExtensions {
		if err = c.executeCreateExtension(extName, schema); err != nil {
			return err
		}
	}
	for extName, schema := range alterExtensions {
		if err = c.executeAlterExtension(extName, schema); err != nil {
			return err
		}
	}

	return nil
}

func (c *Cluster) syncLogicalBackupJob() error {
	var (
		job        *batchv1beta1.CronJob
		desiredJob *batchv1beta1.CronJob
		err        error
	)
	c.setProcessName("syncing the logical backup job")

	// sync the job if it exists

	jobName := c.getLogicalBackupJobName()
	if job, err = c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{}); err == nil {

		desiredJob, err = c.generateLogicalBackupJob()
		if err != nil {
			return fmt.Errorf("could not generate the desired logical backup job state: %v", err)
		}
		if match, reason := k8sutil.SameLogicalBackupJob(job, desiredJob); !match {
			c.logger.Infof("logical job %s is not in the desired state and needs to be updated",
				c.getLogicalBackupJobName(),
			)
			if reason != "" {
				c.logger.Infof("reason: %s", reason)
			}
			if err = c.patchLogicalBackupJob(desiredJob); err != nil {
				return fmt.Errorf("could not update logical backup job to match desired state: %v", err)
			}
			c.logger.Info("the logical backup job is synced")
		}
		return nil
	}
	if !k8sutil.ResourceNotFound(err) {
		return fmt.Errorf("could not get logical backp job: %v", err)
	}

	// no existing logical backup job, create new one
	c.logger.Info("could not find the cluster's logical backup job")

	if err = c.createLogicalBackupJob(); err == nil {
		c.logger.Infof("created missing logical backup job %s", jobName)
	} else {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not create missing logical backup job: %v", err)
		}
		c.logger.Infof("logical backup job %s already exists", jobName)
		if _, err = c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("could not fetch existing logical backup job: %v", err)
		}
	}

	return nil
}
