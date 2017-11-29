package cluster

import (
	"encoding/json"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	policybeta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
)

const (
	pgBinariesLocationTemplate       = "/usr/lib/postgresql/%s/bin"
	patroniPGBinariesParameterName   = "bin_dir"
	patroniPGParametersParameterName = "parameters"
	localHost                        = "127.0.0.1/32"
)

type pgUser struct {
	Password string   `json:"password"`
	Options  []string `json:"options"`
}

type patroniDCS struct {
	TTL                  uint32  `json:"ttl,omitempty"`
	LoopWait             uint32  `json:"loop_wait,omitempty"`
	RetryTimeout         uint32  `json:"retry_timeout,omitempty"`
	MaximumLagOnFailover float32 `json:"maximum_lag_on_failover,omitempty"`
}

type pgBootstrap struct {
	Initdb []interface{}     `json:"initdb"`
	Users  map[string]pgUser `json:"users"`
	PgHBA  []string          `json:"pg_hba"`
	DCS    patroniDCS        `json:"dcs,omitempty"`
}

type spiloConfiguration struct {
	PgLocalConfiguration map[string]interface{} `json:"postgresql"`
	Bootstrap            pgBootstrap            `json:"bootstrap"`
}

func (c *Cluster) containerName() string {
	return c.Name
}

func (c *Cluster) statefulSetName() string {
	return c.Name
}

func (c *Cluster) endpointName(role PostgresRole) string {
	name := c.Name
	if role == Replica {
		name = name + "-repl"
	}

	return name
}

func (c *Cluster) serviceName(role PostgresRole) string {
	name := c.Name
	if role == Replica {
		name = name + "-repl"
	}

	return name
}

func (c *Cluster) podDisruptionBudgetName() string {
	return c.OpConfig.PDBNameFormat.Format("cluster", c.Name)
}

func (c *Cluster) resourceRequirements(resources spec.Resources) (*v1.ResourceRequirements, error) {
	var err error

	specRequests := resources.ResourceRequest
	specLimits := resources.ResourceLimits

	config := c.OpConfig

	defaultRequests := spec.ResourceDescription{CPU: config.DefaultCPURequest, Memory: config.DefaultMemoryRequest}
	defaultLimits := spec.ResourceDescription{CPU: config.DefaultCPULimit, Memory: config.DefaultMemoryLimit}

	result := v1.ResourceRequirements{}

	result.Requests, err = fillResourceList(specRequests, defaultRequests)
	if err != nil {
		return nil, fmt.Errorf("could not fill resource requests: %v", err)
	}

	result.Limits, err = fillResourceList(specLimits, defaultLimits)
	if err != nil {
		return nil, fmt.Errorf("could not fill resource limits: %v", err)
	}

	return &result, nil
}

func fillResourceList(spec spec.ResourceDescription, defaults spec.ResourceDescription) (v1.ResourceList, error) {
	var err error
	requests := v1.ResourceList{}

	if spec.CPU != "" {
		requests[v1.ResourceCPU], err = resource.ParseQuantity(spec.CPU)
		if err != nil {
			return nil, fmt.Errorf("could not parse CPU quantity: %v", err)
		}
	} else {
		requests[v1.ResourceCPU], err = resource.ParseQuantity(defaults.CPU)
		if err != nil {
			return nil, fmt.Errorf("could not parse default CPU quantity: %v", err)
		}
	}
	if spec.Memory != "" {
		requests[v1.ResourceMemory], err = resource.ParseQuantity(spec.Memory)
		if err != nil {
			return nil, fmt.Errorf("could not parse memory quantity: %v", err)
		}
	} else {
		requests[v1.ResourceMemory], err = resource.ParseQuantity(defaults.Memory)
		if err != nil {
			return nil, fmt.Errorf("could not parse default memory quantity: %v", err)
		}
	}

	return requests, nil
}

func (c *Cluster) generateSpiloJSONConfiguration(pg *spec.PostgresqlParam, patroni *spec.Patroni) string {
	config := spiloConfiguration{}

	config.Bootstrap = pgBootstrap{}

	config.Bootstrap.Initdb = []interface{}{map[string]string{"auth-host": "md5"},
		map[string]string{"auth-local": "trust"}}

	initdbOptionNames := []string{}

	for k := range patroni.InitDB {
		initdbOptionNames = append(initdbOptionNames, k)
	}
	/* We need to sort the user-defined options to more easily compare the resulting specs */
	sort.Strings(initdbOptionNames)

	// Initdb parameters in the manifest take priority over the default ones
	// The whole type switch dance is caused by the ability to specify both
	// maps and normal string items in the array of initdb options. We need
	// both to convert the initial key-value to strings when necessary, and
	// to de-duplicate the options supplied.
PatroniInitDBParams:
	for _, k := range initdbOptionNames {
		v := patroni.InitDB[k]
		for i, defaultParam := range config.Bootstrap.Initdb {
			switch defaultParam.(type) {
			case map[string]string:
				{
					for k1 := range defaultParam.(map[string]string) {
						if k1 == k {
							(config.Bootstrap.Initdb[i]).(map[string]string)[k] = v
							continue PatroniInitDBParams
						}
					}
				}
			case string:
				{
					/* if the option already occurs in the list */
					if defaultParam.(string) == v {
						continue PatroniInitDBParams
					}
				}
			default:
				c.logger.Warningf("unsupported type for initdb configuration item %s: %T", defaultParam, defaultParam)
				continue PatroniInitDBParams
			}
		}
		// The following options are known to have no parameters
		if v == "true" {
			switch k {
			case "data-checksums", "debug", "no-locale", "noclean", "nosync", "sync-only":
				config.Bootstrap.Initdb = append(config.Bootstrap.Initdb, k)
				continue
			}
		}
		config.Bootstrap.Initdb = append(config.Bootstrap.Initdb, map[string]string{k: v})
	}

	// pg_hba parameters in the manifest replace the default ones. We cannot
	// reasonably merge them automatically, because pg_hba parsing stops on
	// a first successfully matched rule.
	if len(patroni.PgHba) > 0 {
		config.Bootstrap.PgHBA = patroni.PgHba
	} else {
		config.Bootstrap.PgHBA = []string{
			"hostnossl all all all reject",
			fmt.Sprintf("hostssl   all +%s all pam", c.OpConfig.PamRoleName),
			"hostssl   all all all md5",
		}
	}

	if patroni.MaximumLagOnFailover >= 0 {
		config.Bootstrap.DCS.MaximumLagOnFailover = patroni.MaximumLagOnFailover
	}
	if patroni.LoopWait != 0 {
		config.Bootstrap.DCS.LoopWait = patroni.LoopWait
	}
	if patroni.RetryTimeout != 0 {
		config.Bootstrap.DCS.RetryTimeout = patroni.RetryTimeout
	}
	if patroni.TTL != 0 {
		config.Bootstrap.DCS.TTL = patroni.TTL
	}

	config.PgLocalConfiguration = make(map[string]interface{})
	config.PgLocalConfiguration[patroniPGBinariesParameterName] = fmt.Sprintf(pgBinariesLocationTemplate, pg.PgVersion)
	if len(pg.Parameters) > 0 {
		config.PgLocalConfiguration[patroniPGParametersParameterName] = pg.Parameters
	}
	config.Bootstrap.Users = map[string]pgUser{
		c.OpConfig.PamRoleName: {
			Password: "",
			Options:  []string{constants.RoleFlagCreateDB, constants.RoleFlagNoLogin},
		},
	}
	result, err := json.Marshal(config)
	if err != nil {
		c.logger.Errorf("cannot convert spilo configuration into JSON: %v", err)
		return ""
	}
	return string(result)
}

func (c *Cluster) nodeAffinity() *v1.Affinity {
	matchExpressions := make([]v1.NodeSelectorRequirement, 0)
	for k, v := range c.OpConfig.EOLNodeLabel {
		matchExpressions = append(matchExpressions, v1.NodeSelectorRequirement{
			Key:      k,
			Operator: v1.NodeSelectorOpNotIn,
			Values:   []string{v},
		})
	}

	return &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{MatchExpressions: matchExpressions}},
			},
		},
	}
}

func (c *Cluster) tolerations(tolerationsSpec *[]v1.Toleration) []v1.Toleration {
	// allow to override tolerations by postgresql manifest
	if len(*tolerationsSpec) > 0 {
		return *tolerationsSpec
	}

	podToleration := c.Config.OpConfig.PodToleration
	if len(podToleration["key"]) > 0 || len(podToleration["operator"]) > 0 || len(podToleration["value"]) > 0 || len(podToleration["effect"]) > 0 {
		return []v1.Toleration{
			{
				Key:      podToleration["key"],
				Operator: v1.TolerationOperator(podToleration["operator"]),
				Value:    podToleration["value"],
				Effect:   v1.TaintEffect(podToleration["effect"]),
			},
		}
	} else {
		return []v1.Toleration{}
	}
}

func (c *Cluster) generatePodTemplate(resourceRequirements *v1.ResourceRequirements,
	tolerationsSpec *[]v1.Toleration,
	pgParameters *spec.PostgresqlParam,
	patroniParameters *spec.Patroni,
	cloneDescription *spec.CloneDescription,
	dockerImage *string) *v1.PodTemplateSpec {
	spiloConfiguration := c.generateSpiloJSONConfiguration(pgParameters, patroniParameters)

	envVars := []v1.EnvVar{
		{
			Name:  "SCOPE",
			Value: c.Name,
		},
		{
			Name:  "PGROOT",
			Value: constants.PostgresDataPath,
		},
		{
			Name: "POD_IP",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "status.podIP",
				},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.namespace",
				},
			},
		},
		{
			Name:  "PGUSER_SUPERUSER",
			Value: c.OpConfig.SuperUsername,
		},
		{
			Name: "PGPASSWORD_SUPERUSER",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: c.credentialSecretName(c.OpConfig.SuperUsername),
					},
					Key: "password",
				},
			},
		},
		{
			Name:  "PGUSER_STANDBY",
			Value: c.OpConfig.ReplicationUsername,
		},
		{
			Name: "PGPASSWORD_STANDBY",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: c.credentialSecretName(c.OpConfig.ReplicationUsername),
					},
					Key: "password",
				},
			},
		},
		{
			Name:  "PAM_OAUTH2",
			Value: c.OpConfig.PamConfiguration,
		},
	}
	if spiloConfiguration != "" {
		envVars = append(envVars, v1.EnvVar{Name: "SPILO_CONFIGURATION", Value: spiloConfiguration})
	}
	if c.OpConfig.WALES3Bucket != "" {
		envVars = append(envVars, v1.EnvVar{Name: "WAL_S3_BUCKET", Value: c.OpConfig.WALES3Bucket})
	}

	if c.OpConfig.EtcdHost == "" {
		envVars = append(envVars, v1.EnvVar{Name: "DCS_ENABLE_KUBERNETES_API", Value: "true"})
	} else {
		envVars = append(envVars, v1.EnvVar{Name: "ETCD_HOST", Value: c.OpConfig.EtcdHost})
	}

	if cloneDescription.ClusterName != "" {
		envVars = append(envVars, c.generateCloneEnvironment(cloneDescription)...)
	}

	envFromSource := []v1.EnvFromSource{}
	if c.OpConfig.PodEnvironmentConfigMap != "" {
		configMapRef := v1.ConfigMapEnvSource{LocalObjectReference: v1.LocalObjectReference{Name: c.OpConfig.PodEnvironmentConfigMap}}
		envFromSource = append(envFromSource, v1.EnvFromSource{ConfigMapRef: &configMapRef})
	}

	privilegedMode := true
	containerImage := c.OpConfig.DockerImage
	if dockerImage != nil && *dockerImage != "" {
		containerImage = *dockerImage
	}
	container := v1.Container{
		Name:            c.containerName(),
		Image:           containerImage,
		ImagePullPolicy: v1.PullIfNotPresent,
		Resources:       *resourceRequirements,
		Ports: []v1.ContainerPort{
			{
				ContainerPort: 8008,
				Protocol:      v1.ProtocolTCP,
			},
			{
				ContainerPort: 5432,
				Protocol:      v1.ProtocolTCP,
			},
			{
				ContainerPort: 8080,
				Protocol:      v1.ProtocolTCP,
			},
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      constants.DataVolumeName,
				MountPath: constants.PostgresDataMount, //TODO: fetch from manifesto
			},
		},
		Env: envVars,
		EnvFrom: envFromSource,
		SecurityContext: &v1.SecurityContext{
			Privileged: &privilegedMode,
		},
	}
	terminateGracePeriodSeconds := int64(c.OpConfig.PodTerminateGracePeriod.Seconds())

	podSpec := v1.PodSpec{
		ServiceAccountName:            c.OpConfig.ServiceAccountName,
		TerminationGracePeriodSeconds: &terminateGracePeriodSeconds,
		Containers:                    []v1.Container{container},
		Affinity:                      c.nodeAffinity(),
		Tolerations:                   c.tolerations(tolerationsSpec),
	}

	template := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    c.labelsSet(),
			Namespace: c.Namespace,
		},
		Spec: podSpec,
	}
	if c.OpConfig.KubeIAMRole != "" {
		template.Annotations = map[string]string{constants.KubeIAmAnnotation: c.OpConfig.KubeIAMRole}
	}

	return &template
}

func (c *Cluster) generateStatefulSet(spec *spec.PostgresSpec) (*v1beta1.StatefulSet, error) {
	resourceRequirements, err := c.resourceRequirements(spec.Resources)
	if err != nil {
		return nil, fmt.Errorf("could not generate resource requirements: %v", err)
	}

	podTemplate := c.generatePodTemplate(resourceRequirements, &spec.Tolerations, &spec.PostgresqlParam, &spec.Patroni, &spec.Clone, &spec.DockerImage)
	volumeClaimTemplate, err := generatePersistentVolumeClaimTemplate(spec.Volume.Size, spec.Volume.StorageClass)
	if err != nil {
		return nil, fmt.Errorf("could not generate volume claim template: %v", err)
	}

	statefulSet := &v1beta1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.statefulSetName(),
			Namespace: c.Namespace,
			Labels:    c.labelsSet(),
		},
		Spec: v1beta1.StatefulSetSpec{
			Replicas:             &spec.NumberOfInstances,
			ServiceName:          c.serviceName(Master),
			Template:             *podTemplate,
			VolumeClaimTemplates: []v1.PersistentVolumeClaim{*volumeClaimTemplate},
		},
	}

	return statefulSet, nil
}

func generatePersistentVolumeClaimTemplate(volumeSize, volumeStorageClass string) (*v1.PersistentVolumeClaim, error) {
	metadata := metav1.ObjectMeta{
		Name: constants.DataVolumeName,
	}
	if volumeStorageClass != "" {
		// TODO: check if storage class exists
		metadata.Annotations = map[string]string{"volume.beta.kubernetes.io/storage-class": volumeStorageClass}
	} else {
		metadata.Annotations = map[string]string{"volume.alpha.kubernetes.io/storage-class": "default"}
	}

	quantity, err := resource.ParseQuantity(volumeSize)
	if err != nil {
		return nil, fmt.Errorf("could not parse volume size: %v", err)
	}

	volumeClaim := &v1.PersistentVolumeClaim{
		ObjectMeta: metadata,
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: quantity,
				},
			},
		},
	}

	return volumeClaim, nil
}

func (c *Cluster) generateUserSecrets() (secrets map[string]*v1.Secret) {
	secrets = make(map[string]*v1.Secret, len(c.pgUsers))
	namespace := c.Namespace
	for username, pgUser := range c.pgUsers {
		//Skip users with no password i.e. human users (they'll be authenticated using pam)
		secret := c.generateSingleUserSecret(namespace, pgUser)
		if secret != nil {
			secrets[username] = secret
		}
	}
	/* special case for the system user */
	for _, systemUser := range c.systemUsers {
		secret := c.generateSingleUserSecret(namespace, systemUser)
		if secret != nil {
			secrets[systemUser.Name] = secret
		}
	}

	return
}

func (c *Cluster) generateSingleUserSecret(namespace string, pgUser spec.PgUser) *v1.Secret {
	//Skip users with no password i.e. human users (they'll be authenticated using pam)
	if pgUser.Password == "" {
		return nil
	}
	username := pgUser.Name
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.credentialSecretName(username),
			Namespace: namespace,
			Labels:    c.labelsSet(),
		},
		Type: v1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte(pgUser.Name),
			"password": []byte(pgUser.Password),
		},
	}
	return &secret
}

func (c *Cluster) generateService(role PostgresRole, spec *spec.PostgresSpec) *v1.Service {
	var dnsName string

	if role == Master {
		dnsName = c.masterDNSName()
	} else {
		dnsName = c.replicaDNSName()
	}

	serviceSpec := v1.ServiceSpec{
		Ports: []v1.ServicePort{{Name: "postgresql", Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
		Type:  v1.ServiceTypeClusterIP,
	}

	if role == Replica {
		serviceSpec.Selector = c.roleLabelsSet(role)
	}

	var annotations map[string]string

	// Examine the per-cluster load balancer setting, if it is not defined - check the operator configuration.
	if (spec.UseLoadBalancer != nil && *spec.UseLoadBalancer) ||
		(spec.UseLoadBalancer == nil && c.OpConfig.EnableLoadBalancer) {

		// safe default value: lock load balancer to only local address unless overridden explicitly.
		sourceRanges := []string{localHost}
		allowedSourceRanges := spec.AllowedSourceRanges
		if len(allowedSourceRanges) >= 0 {
			sourceRanges = allowedSourceRanges
		}

		serviceSpec.Type = v1.ServiceTypeLoadBalancer
		serviceSpec.LoadBalancerSourceRanges = sourceRanges

		annotations = map[string]string{
			constants.ZalandoDNSNameAnnotation: dnsName,
			constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
		}
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        c.serviceName(role),
			Namespace:   c.Namespace,
			Labels:      c.roleLabelsSet(role),
			Annotations: annotations,
		},
		Spec: serviceSpec,
	}

	return service
}

func (c *Cluster) generateEndpoint(role PostgresRole, subsets []v1.EndpointSubset) *v1.Endpoints {
	endpoints := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.endpointName(role),
			Namespace: c.Namespace,
			Labels:    c.roleLabelsSet(role),
		},
	}
	if len(subsets) > 0 {
		endpoints.Subsets = subsets
	}

	return endpoints
}

func (c *Cluster) generateCloneEnvironment(description *spec.CloneDescription) []v1.EnvVar {
	result := make([]v1.EnvVar, 0)

	if description.ClusterName == "" {
		return result
	}

	cluster := description.ClusterName
	result = append(result, v1.EnvVar{Name: "CLONE_SCOPE", Value: cluster})
	if description.EndTimestamp == "" {
		// cloning with basebackup, make a connection string to the cluster to clone from
		host, port := c.getClusterServiceConnectionParameters(cluster)
		// TODO: make some/all of those constants
		result = append(result, v1.EnvVar{Name: "CLONE_METHOD", Value: "CLONE_WITH_BASEBACKUP"})
		result = append(result, v1.EnvVar{Name: "CLONE_HOST", Value: host})
		result = append(result, v1.EnvVar{Name: "CLONE_PORT", Value: port})
		// TODO: assume replication user name is the same for all clusters, fetch it from secrets otherwise
		result = append(result, v1.EnvVar{Name: "CLONE_USER", Value: c.OpConfig.ReplicationUsername})
		result = append(result,
			v1.EnvVar{Name: "CLONE_PASSWORD",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: c.credentialSecretNameForCluster(c.OpConfig.ReplicationUsername,
								description.ClusterName),
						},
						Key: "password",
					},
				},
			})
	} else {
		// cloning with S3, find out the bucket to clone
		result = append(result, v1.EnvVar{Name: "CLONE_METHOD", Value: "CLONE_WITH_WALE"})
		result = append(result, v1.EnvVar{Name: "CLONE_WAL_S3_BUCKET", Value: c.OpConfig.WALES3Bucket})
		result = append(result, v1.EnvVar{Name: "CLONE_TARGET_TIME", Value: description.EndTimestamp})
	}

	return result
}

func (c *Cluster) generatePodDisruptionBudget() *policybeta1.PodDisruptionBudget {
	minAvailable := intstr.FromInt(1)

	return &policybeta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.podDisruptionBudgetName(),
			Namespace: c.Namespace,
			Labels:    c.labelsSet(),
		},
		Spec: policybeta1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: c.roleLabelsSet(Master),
			},
		},
	}
}

// getClusterServiceConnectionParameters fetches cluster host name and port
// TODO: perhaps we need to query the service (i.e. if non-standard port is used?)
// TODO: handle clusters in different namespaces
func (c *Cluster) getClusterServiceConnectionParameters(clusterName string) (host string, port string) {
	host = clusterName
	port = "5432"
	return
}
