package cluster

import (
	"context"
	"fmt"
	"reflect"
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

func statefulsetsEqual(ss1, ss2 *v1beta1.StatefulSet) (equal bool, needsRollUpdate bool) {
	equal = true
	needsRollUpdate = false
	//TODO: improve me
	if *ss1.Spec.Replicas != *ss2.Spec.Replicas {
		equal = false
	}
	if len(ss1.Spec.Template.Spec.Containers) != len(ss1.Spec.Template.Spec.Containers) {
		equal = false
		needsRollUpdate = true
		return
	}
	if len(ss1.Spec.Template.Spec.Containers) == 0 {
		return
	}

	container1 := ss1.Spec.Template.Spec.Containers[0]
	container2 := ss2.Spec.Template.Spec.Containers[0]
	if container1.Image != container2.Image {
		equal = false
		needsRollUpdate = true
		return
	}

	if !reflect.DeepEqual(container1.Ports, container2.Ports) {
		equal = false
		needsRollUpdate = true
		return
	}

	if !reflect.DeepEqual(container1.Resources, container2.Resources) {
		equal = false
		needsRollUpdate = true
		return
	}
	if !reflect.DeepEqual(container1.Env, container2.Env) {
		equal = false
		needsRollUpdate = true
	}

	return
}

func servicesEqual(svc1, svc2 *v1.Service) bool {
	//TODO: check of Ports
	//TODO: improve me
	if reflect.DeepEqual(svc1.Spec.LoadBalancerSourceRanges, svc2.Spec.LoadBalancerSourceRanges) {
		return true
	}

	return false
}

func podMatchesTemplate(pod *v1.Pod, ss *v1beta1.StatefulSet) bool {
	//TODO: improve me
	if len(pod.Spec.Containers) != 1 {
		return false
	}
	container := pod.Spec.Containers[0]
	ssContainer := ss.Spec.Template.Spec.Containers[0]

	if container.Image != ssContainer.Image {
		return false
	}
	if !reflect.DeepEqual(container.Env, ssContainer.Env) {
		return false
	}
	if !reflect.DeepEqual(container.Ports, ssContainer.Ports) {
		return false
	}
	if !reflect.DeepEqual(container.Resources, ssContainer.Resources) {
		return false
	}

	return true
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
			role := util.PodSpiloRole(podEvent.CurPod)
			if role == spiloRole { // TODO: newly-created Pods are always replicas => check against empty string only
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
	return retryutil.Retry(constants.ResourceCheckInterval, constants.ResourceCheckTimeout,
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

	return retryutil.Retry(constants.ResourceCheckInterval, constants.ResourceCheckTimeout,
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
