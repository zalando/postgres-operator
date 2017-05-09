package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/resource"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/pkg/util/intstr"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

func (c *Cluster) resourceRequirements(resources spec.Resources) *v1.ResourceRequirements {
	specRequests := resources.ResourceRequest
	specLimits := resources.ResourceLimits

	config := c.OpConfig

	defaultRequests := spec.ResourceDescription{Cpu: config.DefaultCpuRequest, Memory: config.DefaultMemoryRequest}
	defaultLimits := spec.ResourceDescription{Cpu: config.DefaultCpuLimit, Memory: config.DefaultMemoryLimit}

	result := v1.ResourceRequirements{}

	result.Requests = fillResourceList(specRequests, defaultRequests)
	result.Limits = fillResourceList(specLimits, defaultLimits)

	return &result
}

func fillResourceList(spec spec.ResourceDescription, defaults spec.ResourceDescription) v1.ResourceList {
	requests := v1.ResourceList{}

	if spec.Cpu != "" {
		requests[v1.ResourceCPU] = resource.MustParse(spec.Cpu)
	} else {
		requests[v1.ResourceCPU] = resource.MustParse(defaults.Cpu)
	}

	if spec.Memory != "" {
		requests[v1.ResourceMemory] = resource.MustParse(spec.Memory)
	} else {
		requests[v1.ResourceMemory] = resource.MustParse(defaults.Memory)
	}
	return requests
}

func (c *Cluster) genPodTemplate(resourceRequirements *v1.ResourceRequirements, pgVersion string) *v1.PodTemplateSpec {
	envVars := []v1.EnvVar{
		{
			Name:  "SCOPE",
			Value: c.Metadata.Name,
		},
		{
			Name:  "PGROOT",
			Value: "/home/postgres/pgdata/pgroot",
		},
		{
			Name:  "ETCD_HOST",
			Value: c.OpConfig.EtcdHost,
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
		{
			Name: "SPILO_CONFIGURATION",
			Value: fmt.Sprintf(`
postgresql:
  bin_dir: /usr/lib/postgresql/%s/bin
bootstrap:
  initdb:
  - auth-host: md5
  - auth-local: trust
  users:
    %s:
      password: NULL
      options:
        - createdb
        - nologin
  pg_hba:
  - hostnossl all all all reject
  - hostssl   all +%s all pam
  - hostssl   all all all md5`, pgVersion, c.OpConfig.PamRoleName, c.OpConfig.PamRoleName),
		},
	}
	if c.OpConfig.WALES3Bucket != "" {
		envVars = append(envVars, v1.EnvVar{Name: "WAL_S3_BUCKET", Value: c.OpConfig.WALES3Bucket})
	}
	privilegedMode := bool(true)
	container := v1.Container{
		Name:            c.Metadata.Name,
		Image:           c.OpConfig.DockerImage,
		ImagePullPolicy: v1.PullAlways,
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
				MountPath: "/home/postgres/pgdata", //TODO: fetch from manifesto
			},
		},
		Env: envVars,
		SecurityContext: &v1.SecurityContext{
			Privileged: &privilegedMode,
		},
	}
	terminateGracePeriodSeconds := int64(30)

	podSpec := v1.PodSpec{
		ServiceAccountName:            c.OpConfig.ServiceAccountName,
		TerminationGracePeriodSeconds: &terminateGracePeriodSeconds,
		Containers:                    []v1.Container{container},
	}

	template := v1.PodTemplateSpec{
		ObjectMeta: v1.ObjectMeta{
			Labels:    c.labelsSet(),
			Namespace: c.Metadata.Name,
		},
		Spec: podSpec,
	}
	if c.OpConfig.KubeIAMRole != "" {
		template.Annotations = map[string]string{constants.KubeIAmAnnotation: c.OpConfig.KubeIAMRole}
	}

	return &template
}

func (c *Cluster) genStatefulSet(spec spec.PostgresSpec) *v1beta1.StatefulSet {
	resourceRequirements := c.resourceRequirements(spec.Resources)
	podTemplate := c.genPodTemplate(resourceRequirements, spec.PgVersion)
	volumeClaimTemplate := persistentVolumeClaimTemplate(spec.Volume.Size, spec.Volume.StorageClass)

	statefulSet := &v1beta1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      c.Metadata.Name,
			Namespace: c.Metadata.Namespace,
			Labels:    c.labelsSet(),
		},
		Spec: v1beta1.StatefulSetSpec{
			Replicas:             &spec.NumberOfInstances,
			ServiceName:          c.Metadata.Name,
			Template:             *podTemplate,
			VolumeClaimTemplates: []v1.PersistentVolumeClaim{*volumeClaimTemplate},
		},
	}

	return statefulSet
}

func persistentVolumeClaimTemplate(volumeSize, volumeStorageClass string) *v1.PersistentVolumeClaim {
	metadata := v1.ObjectMeta{
		Name: constants.DataVolumeName,
	}
	if volumeStorageClass != "" {
		// TODO: check if storage class exists
		metadata.Annotations = map[string]string{"volume.beta.kubernetes.io/storage-class": volumeStorageClass}
	} else {
		metadata.Annotations = map[string]string{"volume.alpha.kubernetes.io/storage-class": "default"}
	}

	volumeClaim := &v1.PersistentVolumeClaim{
		ObjectMeta: metadata,
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse(volumeSize),
				},
			},
		},
	}
	return volumeClaim
}

func (c *Cluster) genUserSecrets() (secrets map[string]*v1.Secret) {
	secrets = make(map[string]*v1.Secret, len(c.pgUsers))
	namespace := c.Metadata.Namespace
	for username, pgUser := range c.pgUsers {
		//Skip users with no password i.e. human users (they'll be authenticated using pam)
		secret := c.genSingleUserSecret(namespace, pgUser)
		if secret != nil {
			secrets[username] = secret
		}
	}
	/* special case for the system user */
	for _, systemUser := range c.systemUsers {
		secret := c.genSingleUserSecret(namespace, systemUser)
		if secret != nil {
			secrets[systemUser.Name] = secret
		}
	}

	return
}

func (c *Cluster) genSingleUserSecret(namespace string, pgUser spec.PgUser) *v1.Secret {
	//Skip users with no password i.e. human users (they'll be authenticated using pam)
	if pgUser.Password == "" {
		return nil
	}
	username := pgUser.Name
	secret := v1.Secret{
		ObjectMeta: v1.ObjectMeta{
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

func (c *Cluster) genService(allowedSourceRanges []string) *v1.Service {
	service := &v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      c.Metadata.Name,
			Namespace: c.Metadata.Namespace,
			Labels:    c.labelsSet(),
			Annotations: map[string]string{
				constants.ZalandoDnsNameAnnotation: c.dnsName(),
				constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
			},
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{Name: "postgresql", Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
			LoadBalancerSourceRanges: allowedSourceRanges,
		},
	}

	return service
}

func (c *Cluster) genEndpoints() *v1.Endpoints {
	endpoints := &v1.Endpoints{
		ObjectMeta: v1.ObjectMeta{
			Name:      c.Metadata.Name,
			Namespace: c.Metadata.Namespace,
			Labels:    c.labelsSet(),
		},
	}

	return endpoints
}
