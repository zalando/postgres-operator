package cluster

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/retryutil"
)

func isValidUsername(username string) bool {
	return userRegexp.MatchString(username)
}

func normalizeUserFlags(userFlags []string) (flags []string, err error) {
	uniqueFlags := make(map[string]bool)
	addLogin := true

	for _, flag := range userFlags {
		if !alphaNumericRegexp.MatchString(flag) {
			err = fmt.Errorf("user flag '%v' is not alphanumeric", flag)
			return
		}
		flag = strings.ToUpper(flag)
		if _, ok := uniqueFlags[flag]; !ok {
			uniqueFlags[flag] = true
		}
	}
	if uniqueFlags[constants.RoleFlagLogin] && uniqueFlags[constants.RoleFlagNoLogin] {
		return nil, fmt.Errorf("conflicting or redundant flags: LOGIN and NOLOGIN")
	}

	flags = []string{}
	for k := range uniqueFlags {
		if k == constants.RoleFlagNoLogin || k == constants.RoleFlagLogin {
			addLogin = false
			if k == constants.RoleFlagNoLogin {
				// we don't add NOLOGIN to the list of flags to be consistent with what we get
				// from the readPgUsersFromDatabase in SyncUsers
				continue
			}
		}
		flags = append(flags, k)
	}
	if addLogin {
		flags = append(flags, constants.RoleFlagLogin)
	}

	return
}

func specPatch(spec interface{}) ([]byte, error) {
	return json.Marshal(struct {
		Spec interface{} `json:"spec"`
	}{spec})
}

func metadataAnnotationsPatch(annotations map[string]string) string {
	annotationsList := make([]string, 0, len(annotations))

	for name, value := range annotations {
		annotationsList = append(annotationsList, fmt.Sprintf(`"%s":"%s"`, name, value))
	}
	annotationsString := strings.Join(annotationsList, ",")
	// TODO: perhaps use patchStrategy:action json annotation instead of constructing the patch literally.
	return fmt.Sprintf(constants.ServiceMetadataAnnotationReplaceFormat, annotationsString)
}

func (c *Cluster) logStatefulSetChanges(old, new *v1beta1.StatefulSet, isUpdate bool, reasons []string) {
	if isUpdate {
		c.logger.Infof("statefulset %q has been changed",
			util.NameFromMeta(old.ObjectMeta),
		)
	} else {
		c.logger.Infof("statefulset %q is not in the desired state and needs to be updated",
			util.NameFromMeta(old.ObjectMeta),
		)
	}
	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old.Spec, new.Spec))

	if len(reasons) > 0 {
		for _, reason := range reasons {
			c.logger.Infof("reason: %q", reason)
		}
	}
}

func (c *Cluster) logServiceChanges(role postgresRole, old, new *v1.Service, isUpdate bool, reason string) {
	if isUpdate {
		c.logger.Infof("%s service %q has been changed",
			role, util.NameFromMeta(old.ObjectMeta),
		)
	} else {
		c.logger.Infof("%s service %q is not in the desired state and needs to be updated",
			role, util.NameFromMeta(old.ObjectMeta),
		)
	}
	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old.Spec, new.Spec))

	if reason != "" {
		c.logger.Infof("reason: %s", reason)
	}
}

func (c *Cluster) logVolumeChanges(old, new spec.Volume, reason string) {
	c.logger.Infof("volume specification has been changed")
	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old, new))
	if reason != "" {
		c.logger.Infof("reason: %s", reason)
	}
}

func (c *Cluster) getOAuthToken() (string, error) {
	//TODO: we can move this function to the Controller in case it will be needed there. As for now we use it only in the Cluster
	// Temporary getting postgresql-operator secret from the NamespaceDefault
	credentialsSecret, err := c.KubeClient.
		Secrets(c.OpConfig.OAuthTokenSecretName.Namespace).
		Get(c.OpConfig.OAuthTokenSecretName.Name, metav1.GetOptions{})

	if err != nil {
		c.logger.Debugf("oauth token secret name: %q", c.OpConfig.OAuthTokenSecretName)
		return "", fmt.Errorf("could not get credentials secret: %v", err)
	}
	data := credentialsSecret.Data

	if string(data["read-only-token-type"]) != "Bearer" {
		return "", fmt.Errorf("wrong token type: %v", data["read-only-token-type"])
	}

	return string(data["read-only-token-secret"]), nil
}

func (c *Cluster) getTeamMembers() ([]string, error) {
	if c.Spec.TeamID == "" {
		return nil, fmt.Errorf("no teamId specified")
	}
	if !c.OpConfig.EnableTeamsAPI {
		c.logger.Debug("team API is disabled, returning empty list of members")
		return []string{}, nil
	}

	token, err := c.getOAuthToken()
	if err != nil {
		return []string{}, fmt.Errorf("could not get oauth token: %v", err)
	}

	teamInfo, err := c.teamsAPIClient.TeamInfo(c.Spec.TeamID, token)
	if err != nil {
		return nil, fmt.Errorf("could not get team info: %v", err)
	}

	return teamInfo.Members, nil
}

func (c *Cluster) waitForPodLabel(podEvents chan spec.PodEvent) error {
	for {
		select {
		case podEvent := <-podEvents:
			role := c.podSpiloRole(podEvent.CurPod)
			// We cannot assume any role of the newly created pod. Normally, for a multi-pod cluster
			// we should observe the 'replica' value, but it could be that some pods are not allowed
			// to promote, therefore, the new pod could be a master as well.
			if role == constants.PodRoleMaster || role == constants.PodRoleReplica {
				return nil
			}
		case <-time.After(c.OpConfig.PodLabelWaitTimeout):
			return fmt.Errorf("pod label wait timeout")
		}
	}
}

func (c *Cluster) waitForPodDeletion(podEvents chan spec.PodEvent) error {
	for {
		select {
		case podEvent := <-podEvents:
			if podEvent.EventType == spec.EventDelete {
				return nil
			}
		case <-time.After(c.OpConfig.PodDeletionWaitTimeout):
			return fmt.Errorf("pod deletion wait timeout")
		}
	}
}

func (c *Cluster) waitStatefulsetReady() error {
	return retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			listOptions := metav1.ListOptions{
				LabelSelector: c.labelsSet().String(),
			}
			ss, err := c.KubeClient.StatefulSets(c.Namespace).List(listOptions)
			if err != nil {
				return false, err
			}

			if len(ss.Items) != 1 {
				return false, fmt.Errorf("statefulset is not found")
			}

			return *ss.Items[0].Spec.Replicas == ss.Items[0].Status.Replicas, nil
		})
}

func (c *Cluster) waitPodLabelsReady() error {
	ls := c.labelsSet()
	namespace := c.Namespace

	listOptions := metav1.ListOptions{
		LabelSelector: ls.String(),
	}
	masterListOption := metav1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{
			c.OpConfig.PodRoleLabel: constants.PodRoleMaster,
		}).String(),
	}
	replicaListOption := metav1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{
			c.OpConfig.PodRoleLabel: constants.PodRoleReplica,
		}).String(),
	}
	pods, err := c.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return err
	}
	podsNumber := len(pods.Items)

	err = retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			masterPods, err2 := c.KubeClient.Pods(namespace).List(masterListOption)
			if err2 != nil {
				return false, err2
			}
			replicaPods, err2 := c.KubeClient.Pods(namespace).List(replicaListOption)
			if err2 != nil {
				return false, err2
			}
			if len(masterPods.Items) > 1 {
				return false, fmt.Errorf("too many masters")
			}
			if len(replicaPods.Items) == podsNumber {
				c.masterLess = true
				return true, nil
			}

			return len(masterPods.Items)+len(replicaPods.Items) == podsNumber, nil
		})

	//TODO: wait for master for a while and then set masterLess flag

	return err
}

func (c *Cluster) waitStatefulsetPodsReady() error {
	// TODO: wait for the first Pod only
	if err := c.waitStatefulsetReady(); err != nil {
		return fmt.Errorf("statuful set error: %v", err)
	}

	// TODO: wait only for master
	if err := c.waitPodLabelsReady(); err != nil {
		return fmt.Errorf("pod labels error: %v", err)
	}

	return nil
}

func (c *Cluster) labelsSet() labels.Set {
	lbls := make(map[string]string)
	for k, v := range c.OpConfig.ClusterLabels {
		lbls[k] = v
	}
	lbls[c.OpConfig.ClusterNameLabel] = c.Name

	return labels.Set(lbls)
}

func (c *Cluster) roleLabelsSet(role postgresRole) labels.Set {
	lbls := c.labelsSet()
	lbls[c.OpConfig.PodRoleLabel] = string(role)
	return lbls
}

func (c *Cluster) masterDNSName() string {
	return strings.ToLower(c.OpConfig.MasterDNSNameFormat.Format(
		"cluster", c.Spec.ClusterName,
		"team", c.teamName(),
		"hostedzone", c.OpConfig.DbHostedZone))
}

func (c *Cluster) replicaDNSName() string {
	return strings.ToLower(c.OpConfig.ReplicaDNSNameFormat.Format(
		"cluster", c.Spec.ClusterName,
		"team", c.teamName(),
		"hostedzone", c.OpConfig.DbHostedZone))
}

func (c *Cluster) credentialSecretName(username string) string {
	// secret  must consist of lower case alphanumeric characters, '-' or '.',
	// and must start and end with an alphanumeric character
	return fmt.Sprintf(constants.UserSecretTemplate,
		strings.Replace(username, "_", "-", -1),
		c.Name)
}

func (c *Cluster) podSpiloRole(pod *v1.Pod) string {
	return pod.Labels[c.OpConfig.PodRoleLabel]
}
