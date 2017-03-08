package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/labels"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/retryutil"
)

func isValidUsername(username string) bool {
	return alphaNumericRegexp.MatchString(username)
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

func (c *Cluster) getTeamMembers() ([]string, error) {
	teamInfo, err := c.config.TeamsAPIClient.TeamInfo(c.Spec.TeamId)
	if err != nil {
		return nil, fmt.Errorf("Can't get team info: %s", err)
	}

	return teamInfo.Members, nil
}

func (c *Cluster) waitForPodLabel(podEvents chan spec.PodEvent, spiloRole string) error {
	for {
		select {
		case podEvent := <-podEvents:
			podLabels := podEvent.CurPod.Labels
			c.logger.Debugf("Pod has following labels: %+v", podLabels)
			val, ok := podLabels["spilo-role"]
			if ok && val == spiloRole {
				return nil
			}
		case <-time.After(constants.PodLabelWaitTimeout):
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
		case <-time.After(constants.PodDeletionWaitTimeout):
			return fmt.Errorf("Pod deletion wait timeout")
		}
	}
}

func (c *Cluster) waitStatefulsetReady() error {
	return retryutil.Retry(constants.ResourceCheckInterval, int(constants.ResourceCheckTimeout/constants.ResourceCheckInterval),
		func() (bool, error) {
			listOptions := v1.ListOptions{
				LabelSelector: c.labelsSet().String(),
			}
			ss, err := c.config.KubeClient.StatefulSets(c.Metadata.Namespace).List(listOptions)
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
		LabelSelector: labels.Merge(ls, labels.Set{"spilo-role": "master"}).String(),
	}
	replicaListOption := v1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{"spilo-role": "replica"}).String(),
	}
	pods, err := c.config.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return err
	}
	podsNumber := len(pods.Items)

	return retryutil.Retry(
		constants.ResourceCheckInterval, int(constants.ResourceCheckTimeout/constants.ResourceCheckInterval),
		func() (bool, error) {
			masterPods, err := c.config.KubeClient.Pods(namespace).List(masterListOption)
			if err != nil {
				return false, err
			}
			replicaPods, err := c.config.KubeClient.Pods(namespace).List(replicaListOption)
			if err != nil {
				return false, err
			}
			if len(masterPods.Items) > 1 {
				return false, fmt.Errorf("Too many masters")
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
	return labels.Set{
		"application":   "spilo",
		"spilo-cluster": c.Metadata.Name,
	}
}

func (c *Cluster) credentialSecretName(username string) string {
	return fmt.Sprintf(constants.UserSecretTemplate,
		username,
		c.Metadata.Name,
		constants.TPRName,
		constants.TPRVendor)
}

func (c *Cluster) deleteEtcdKey() error {
	etcdKey := fmt.Sprintf("/service/%s", c.Metadata.Name)

	//TODO: retry multiple times
	resp, err := c.config.EtcdClient.Delete(context.Background(),
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
