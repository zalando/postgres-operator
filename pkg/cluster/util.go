package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/pkg/labels"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/retryutil"
)

func isValidUsername(username string) bool {
	return userRegexp.MatchString(username)
}

func normalizeUserFlags(userFlags []string) (flags []string, err error) {
	uniqueFlags := make(map[string]bool)

	for _, flag := range userFlags {
		if !alphaNumericRegexp.MatchString(flag) {
			err = fmt.Errorf("User flag '%s' is not alphanumeric", flag)
			return
		} else {
			flag = strings.ToUpper(flag)
			if _, ok := uniqueFlags[flag]; !ok {
				uniqueFlags[flag] = true
			}
		}
	}

	flags = []string{}
	for k := range uniqueFlags {
		flags = append(flags, k)
	}

	return
}

func (c *Cluster) logStatefulSetChanges(old, new *v1beta1.StatefulSet, isUpdate bool, reason string) {
	if isUpdate {
		c.logger.Infof("StatefulSet '%s' has been changed",
			util.NameFromMeta(old.ObjectMeta),
		)
	} else {
		c.logger.Infof("StatefulSet '%s' is not in the desired state and needs to be updated",
			util.NameFromMeta(old.ObjectMeta),
		)
	}
	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old.Spec, new.Spec))

	if reason != "" {
		c.logger.Infof("Reason: %s", reason)
	}
}

func (c *Cluster) logServiceChanges(old, new *v1.Service, isUpdate bool, reason string) {
	if isUpdate {
		c.logger.Infof("Service '%s' has been changed",
			util.NameFromMeta(old.ObjectMeta),
		)
	} else {
		c.logger.Infof("Service '%s  is not in the desired state and needs to be updated",
			util.NameFromMeta(old.ObjectMeta),
		)
	}
	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old.Spec, new.Spec))

	if reason != "" {
		c.logger.Infof("Reason: %s", reason)
	}
}

func (c *Cluster) logVolumeChanges(old, new spec.Volume, reason string) {
	c.logger.Infof("Volume specification has been changed")
	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old, new))
	if reason != "" {
		c.logger.Infof("Reason: %s", reason)
	}
}

func (c *Cluster) getTeamMembers() ([]string, error) {
	if c.Spec.TeamId == "" {
		return nil, fmt.Errorf("No teamId specified")
	}
	teamInfo, err := c.TeamsAPIClient.TeamInfo(c.Spec.TeamId)
	if err != nil {
		return nil, fmt.Errorf("Can't get team info: %s", err)
	}
	c.logger.Debugf("Got from the Team API: %+v", *teamInfo)

	return teamInfo.Members, nil
}

func (c *Cluster) waitForPodLabel(podEvents chan spec.PodEvent) error {
	for {
		select {
		case podEvent := <-podEvents:
			role := c.PodSpiloRole(podEvent.CurPod)
			// We cannot assume any role of the newly created pod. Normally, for a multi-pod cluster
			// we should observe the 'replica' value, but it could be that some pods are not allowed
			// to promote, therefore, the new pod could be a master as well.
			if role == constants.PodRoleMaster || role == constants.PodRoleReplica {
				return nil
			}
		case <-time.After(c.OpConfig.PodLabelWaitTimeout):
			return fmt.Errorf("Pod label wait timeout")
		}
	}
}

func (c *Cluster) waitForPodDeletion(podEvents chan spec.PodEvent) error {
	for {
		select {
		case podEvent := <-podEvents:
			if podEvent.EventType == spec.PodEventDelete {
				return nil
			}
		case <-time.After(c.OpConfig.PodDeletionWaitTimeout):
			return fmt.Errorf("Pod deletion wait timeout")
		}
	}
}

func (c *Cluster) waitStatefulsetReady() error {
	return retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			listOptions := v1.ListOptions{
				LabelSelector: c.labelsSet().String(),
			}
			ss, err := c.KubeClient.StatefulSets(c.Metadata.Namespace).List(listOptions)
			if err != nil {
				return false, err
			}

			if len(ss.Items) != 1 {
				return false, fmt.Errorf("StatefulSet is not found")
			}

			return *ss.Items[0].Spec.Replicas == ss.Items[0].Status.Replicas, nil
		})
}

func (c *Cluster) waitPodLabelsReady() error {
	ls := c.labelsSet()
	namespace := c.Metadata.Namespace

	listOptions := v1.ListOptions{
		LabelSelector: ls.String(),
	}
	masterListOption := v1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{
			c.OpConfig.PodRoleLabel: constants.PodRoleMaster,
		}).String(),
	}
	replicaListOption := v1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{
			c.OpConfig.PodRoleLabel: constants.PodRoleReplica,
		}).String(),
	}
	pods, err := c.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return err
	}
	podsNumber := len(pods.Items)

	return retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			masterPods, err := c.KubeClient.Pods(namespace).List(masterListOption)
			if err != nil {
				return false, err
			}
			replicaPods, err := c.KubeClient.Pods(namespace).List(replicaListOption)
			if err != nil {
				return false, err
			}
			if len(masterPods.Items) > 1 {
				return false, fmt.Errorf("Too many masters")
			}
			if len(replicaPods.Items) == podsNumber {
				return false, fmt.Errorf("Cluster has no master")
			}

			return len(masterPods.Items)+len(replicaPods.Items) == podsNumber, nil
		})
}

func (c *Cluster) waitStatefulsetPodsReady() error {
	// TODO: wait for the first Pod only
	if err := c.waitStatefulsetReady(); err != nil {
		return fmt.Errorf("Statuful set error: %s", err)
	}

	// TODO: wait only for master
	if err := c.waitPodLabelsReady(); err != nil {
		return fmt.Errorf("Pod labels error: %s", err)
	}

	return nil
}

func (c *Cluster) labelsSet() labels.Set {
	lbls := c.OpConfig.ClusterLabels
	lbls[c.OpConfig.ClusterNameLabel] = c.Metadata.Name

	return labels.Set(lbls)
}

func (c *Cluster) dnsName() string {
	return strings.ToLower(fmt.Sprintf(
		c.OpConfig.DNSNameFormat,
		c.Spec.ClusterName,
		c.TeamName(),
		c.OpConfig.DbHostedZone))
}

func (c *Cluster) credentialSecretName(username string) string {
	// secret  must consist of lower case alphanumeric characters, '-' or '.',
	// and must start and end with an alphanumeric character
	return fmt.Sprintf(constants.UserSecretTemplate,
		strings.Replace(username, "_", "-", -1),
		c.Metadata.Name,
		constants.TPRName,
		constants.TPRVendor)
}

func (c *Cluster) deleteEtcdKey() error {
	etcdKey := fmt.Sprintf("/%s/%s", c.OpConfig.EtcdScope, c.Metadata.Name)

	//TODO: retry multiple times
	resp, err := c.EtcdClient.Delete(context.Background(),
		etcdKey,
		&etcdclient.DeleteOptions{Recursive: true})

	if err != nil {
		return fmt.Errorf("Can't delete etcd key: %s", err)
	}

	if resp == nil {
		return fmt.Errorf("No response from etcd cluster")
	}

	return nil
}

func (c *Cluster) PodSpiloRole(pod *v1.Pod) string {
	return pod.Labels[c.OpConfig.PodRoleLabel]
}
