package cluster

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	policybeta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/retryutil"
)

// OAuthTokenGetter provides the method for fetching OAuth tokens
type OAuthTokenGetter interface {
	getOAuthToken() (string, error)
}

// SecretOauthTokenGetter enables fetching OAuth tokens by reading Kubernetes secrets
type SecretOauthTokenGetter struct {
	kubeClient           *k8sutil.KubernetesClient
	OAuthTokenSecretName spec.NamespacedName
}

func NewSecretOauthTokenGetter(kubeClient *k8sutil.KubernetesClient,
	OAuthTokenSecretName spec.NamespacedName) *SecretOauthTokenGetter {
	return &SecretOauthTokenGetter{kubeClient, OAuthTokenSecretName}
}

func (g *SecretOauthTokenGetter) getOAuthToken() (string, error) {
	//TODO: we can move this function to the Controller in case it will be needed there. As for now we use it only in the Cluster
	// Temporary getting postgresql-operator secret from the NamespaceDefault
	credentialsSecret, err := g.kubeClient.
		Secrets(g.OAuthTokenSecretName.Namespace).
		Get(g.OAuthTokenSecretName.Name, metav1.GetOptions{})

	if err != nil {
		return "", fmt.Errorf("could not get credentials secret: %v", err)
	}
	data := credentialsSecret.Data

	if string(data["read-only-token-type"]) != "Bearer" {
		return "", fmt.Errorf("wrong token type: %v", data["read-only-token-type"])
	}

	return string(data["read-only-token-secret"]), nil
}

func isValidUsername(username string) bool {
	return userRegexp.MatchString(username)
}

func (c *Cluster) isProtectedUsername(username string) bool {
	for _, protected := range c.OpConfig.ProtectedRoles {
		if username == protected {
			return true
		}
	}
	return false
}

func (c *Cluster) isSystemUsername(username string) bool {
	return (username == c.OpConfig.SuperUsername || username == c.OpConfig.ReplicationUsername)
}

func isValidFlag(flag string) bool {
	for _, validFlag := range []string{constants.RoleFlagSuperuser, constants.RoleFlagLogin, constants.RoleFlagCreateDB,
		constants.RoleFlagInherit, constants.RoleFlagReplication, constants.RoleFlagByPassRLS} {
		if flag == validFlag || flag == "NO"+validFlag {
			return true
		}
	}
	return false
}

func invertFlag(flag string) string {
	if flag[:2] == "NO" {
		return flag[2:]
	}
	return "NO" + flag
}

func normalizeUserFlags(userFlags []string) ([]string, error) {
	uniqueFlags := make(map[string]bool)
	addLogin := true

	for _, flag := range userFlags {
		if !alphaNumericRegexp.MatchString(flag) {
			return nil, fmt.Errorf("user flag %q is not alphanumeric", flag)
		}

		flag = strings.ToUpper(flag)
		if _, ok := uniqueFlags[flag]; !ok {
			if !isValidFlag(flag) {
				return nil, fmt.Errorf("user flag %q is not valid", flag)
			}
			invFlag := invertFlag(flag)
			if uniqueFlags[invFlag] {
				return nil, fmt.Errorf("conflicting user flags: %q and %q", flag, invFlag)
			}
			uniqueFlags[flag] = true
		}
	}

	flags := []string{}
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
	sort.Strings(flags)
	return flags, nil
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

func (c *Cluster) logPDBChanges(old, new *policybeta1.PodDisruptionBudget, isUpdate bool, reason string) {
	if isUpdate {
		c.logger.Infof("pod disruption budget %q has been changed", util.NameFromMeta(old.ObjectMeta))
	} else {
		c.logger.Infof("pod disruption budget %q is not in the desired state and needs to be updated",
			util.NameFromMeta(old.ObjectMeta),
		)
	}

	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old.Spec, new.Spec))
}

func (c *Cluster) logStatefulSetChanges(old, new *v1beta1.StatefulSet, isUpdate bool, reasons []string) {
	if isUpdate {
		c.logger.Infof("statefulset %q has been changed", util.NameFromMeta(old.ObjectMeta))
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

func (c *Cluster) logServiceChanges(role PostgresRole, old, new *v1.Service, isUpdate bool, reason string) {
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

func (c *Cluster) logVolumeChanges(old, new spec.Volume) {
	c.logger.Infof("volume specification has been changed")
	c.logger.Debugf("diff\n%s\n", util.PrettyDiff(old, new))
}

func (c *Cluster) getTeamMembers() ([]string, error) {
	if c.Spec.TeamID == "" {
		return nil, fmt.Errorf("no teamId specified")
	}
	if !c.OpConfig.EnableTeamsAPI {
		c.logger.Debug("team API is disabled, returning empty list of members")
		return []string{}, nil
	}

	token, err := c.oauthTokenGetter.getOAuthToken()
	if err != nil {
		return []string{}, fmt.Errorf("could not get oauth token: %v", err)
	}

	teamInfo, err := c.teamsAPIClient.TeamInfo(c.Spec.TeamID, token)
	if err != nil {
		return nil, fmt.Errorf("could not get team info: %v", err)
	}

	return teamInfo.Members, nil
}

func (c *Cluster) waitForPodLabel(podEvents chan spec.PodEvent, role *PostgresRole) (*v1.Pod, error) {
	timeout := time.After(c.OpConfig.PodLabelWaitTimeout)
	for {
		select {
		case podEvent := <-podEvents:
			podRole := PostgresRole(podEvent.CurPod.Labels[c.OpConfig.PodRoleLabel])

			if role == nil {
				if podRole == Master || podRole == Replica {
					return podEvent.CurPod, nil
				}
			} else if *role == podRole {
				return podEvent.CurPod, nil
			}
		case <-timeout:
			return nil, fmt.Errorf("pod label wait timeout")
		}
	}
}

func (c *Cluster) waitForPodDeletion(podEvents chan spec.PodEvent) error {
	timeout := time.After(c.OpConfig.PodDeletionWaitTimeout)
	for {
		select {
		case podEvent := <-podEvents:
			if podEvent.EventType == spec.EventDelete {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("pod deletion wait timeout")
		}
	}
}

func (c *Cluster) waitStatefulsetReady() error {
	return retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			listOptions := metav1.ListOptions{
				LabelSelector: c.labelsSet(false).String(),
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
	ls := c.labelsSet(false)
	namespace := c.Namespace

	listOptions := metav1.ListOptions{
		LabelSelector: ls.String(),
	}
	masterListOption := metav1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{
			c.OpConfig.PodRoleLabel: string(Master),
		}).String(),
	}
	replicaListOption := metav1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{
			c.OpConfig.PodRoleLabel: string(Replica),
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
				return true, nil
			}

			return len(masterPods.Items)+len(replicaPods.Items) == podsNumber, nil
		})

	return err
}

func (c *Cluster) waitStatefulsetPodsReady() error {
	c.setProcessName("waiting for the pods of the statefulset")
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

// Returns labels used to create or list k8s objects such as pods
// For backward compatability, shouldAddExtraLabels must be false
// when listing k8s objects. See operator PR #252
func (c *Cluster) labelsSet(shouldAddExtraLabels bool) labels.Set {
	lbls := make(map[string]string)
	for k, v := range c.OpConfig.ClusterLabels {
		lbls[k] = v
	}
	lbls[c.OpConfig.ClusterNameLabel] = c.Name

	if shouldAddExtraLabels {
		// enables filtering resources owned by a team
		lbls["team"] = c.Postgresql.Spec.TeamID
	}

	return labels.Set(lbls)
}

func (c *Cluster) roleLabelsSet(role PostgresRole) labels.Set {
	lbls := c.labelsSet(false)
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
	return c.credentialSecretNameForCluster(username, c.Name)
}

func (c *Cluster) credentialSecretNameForCluster(username string, clusterName string) string {
	// secret  must consist of lower case alphanumeric characters, '-' or '.',
	// and must start and end with an alphanumeric character

	return c.OpConfig.SecretNameTemplate.Format(
		"username", strings.Replace(username, "_", "-", -1),
		"cluster", clusterName,
		"tprkind", constants.CRDKind,
		"tprgroup", constants.CRDGroup)
}

func masterCandidate(replicas []spec.NamespacedName) spec.NamespacedName {
	return replicas[rand.Intn(len(replicas))]
}

func cloneSpec(from *spec.Postgresql) (*spec.Postgresql, error) {
	var (
		buf    bytes.Buffer
		result *spec.Postgresql
		err    error
	)
	enc := gob.NewEncoder(&buf)
	if err = enc.Encode(*from); err != nil {
		return nil, fmt.Errorf("could not encode the spec: %v", err)
	}
	dec := gob.NewDecoder(&buf)
	if err = dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("could not decode the spec: %v", err)
	}
	return result, nil
}

func (c *Cluster) setSpec(newSpec *spec.Postgresql) {
	c.specMu.Lock()
	c.Postgresql = *newSpec
	c.specMu.Unlock()
}

func (c *Cluster) GetSpec() (*spec.Postgresql, error) {
	c.specMu.RLock()
	defer c.specMu.RUnlock()
	return cloneSpec(&c.Postgresql)
}

func (c *Cluster) patroniUsesKubernetes() bool {
	return c.OpConfig.EtcdHost == ""
}
