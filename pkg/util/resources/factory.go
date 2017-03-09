package resources

import (
	"fmt"

	"k8s.io/client-go/pkg/api/resource"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	extv1beta "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/labels"
	"k8s.io/client-go/pkg/util/intstr"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

const (
	superuserName       = "postgres"
	replicationUsername = "replication"
	dataVolumeName      = "pgdata"
)

func credentialSecretName(clusterName, username string) string {
	return fmt.Sprintf(
		constants.UserSecretTemplate,
		username,
		clusterName,
		constants.TPRName,
		constants.TPRVendor)
}

func labelsSet(clusterName string) labels.Set {
	return labels.Set{
		"application":   "spilo",
		"spilo-cluster": clusterName,
	}
}

func ResourceList(resources spec.Resources) *v1.ResourceList {
	resourceList := v1.ResourceList{}
	if resources.Cpu != "" {
		resourceList[v1.ResourceCPU] = resource.MustParse(resources.Cpu)
	}

	if resources.Memory != "" {
		resourceList[v1.ResourceMemory] = resource.MustParse(resources.Memory)
	}

	return &resourceList
}

func PodTemplate(cluster spec.ClusterName, resourceList *v1.ResourceList, dockerImage, pgVersion, etcdHost string) *v1.PodTemplateSpec {
	envVars := []v1.EnvVar{
		{
			Name:  "SCOPE",
			Value: cluster.Name,
		},
		{
			Name:  "PGROOT",
			Value: "/home/postgres/pgdata/pgroot",
		},
		{
			Name:  "ETCD_HOST",
			Value: etcdHost,
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
						Name: credentialSecretName(cluster.Name, superuserName),
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
						Name: credentialSecretName(cluster.Name, replicationUsername),
					},
					Key: "password",
				},
			},
		},
		{
			Name:  "PAM_OAUTH2",               //TODO: get from the operator tpr spec
			Value: constants.PamConfiguration, //space before uid is obligatory
		},
		{
			Name: "SPILO_CONFIGURATION", //TODO: get from the operator tpr spec
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
  - hostssl   all all all md5`, pgVersion, constants.PamRoleName, constants.PamRoleName),
		},
	}

	container := v1.Container{
		Name:            cluster.Name,
		Image:           dockerImage,
		ImagePullPolicy: v1.PullAlways,
		Resources: v1.ResourceRequirements{
			Requests: *resourceList,
		},
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
				Name:      dataVolumeName,
				MountPath: "/home/postgres/pgdata", //TODO: fetch from manifesto
			},
		},
		Env: envVars,
	}
	terminateGracePeriodSeconds := int64(30)

	podSpec := v1.PodSpec{
		ServiceAccountName:            constants.ServiceAccountName,
		TerminationGracePeriodSeconds: &terminateGracePeriodSeconds,
		Containers:                    []v1.Container{container},
	}

	template := v1.PodTemplateSpec{
		ObjectMeta: v1.ObjectMeta{
			Labels:      labelsSet(cluster.Name),
			Namespace:   cluster.Namespace,
			Annotations: map[string]string{"pod.alpha.kubernetes.io/initialized": "true"},
		},
		Spec: podSpec,
	}

	return &template
}

func VolumeClaimTemplate(volumeSize, volumeStorageClass string) *v1.PersistentVolumeClaim {
	metadata := v1.ObjectMeta{
		Name: dataVolumeName,
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

func StatefulSet(cluster spec.ClusterName, podTemplate *v1.PodTemplateSpec,
	persistenVolumeClaim *v1.PersistentVolumeClaim, numberOfInstances int32) *v1beta1.StatefulSet {
	statefulSet := &v1beta1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labelsSet(cluster.Name),
		},
		Spec: v1beta1.StatefulSetSpec{
			Replicas:             &numberOfInstances,
			ServiceName:          cluster.Name,
			Template:             *podTemplate,
			VolumeClaimTemplates: []v1.PersistentVolumeClaim{*persistenVolumeClaim},
		},
	}

	return statefulSet
}

func UserSecrets(cluster spec.ClusterName, pgUsers map[string]spec.PgUser) (secrets map[string]*v1.Secret, err error) {
	secrets = make(map[string]*v1.Secret, len(pgUsers))
	namespace := cluster.Namespace
	for username, pgUser := range pgUsers {
		//Skip users with no password i.e. human users (they'll be authenticated using pam)
		if pgUser.Password == "" {
			continue
		}
		secret := v1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      credentialSecretName(cluster.Name, username),
				Namespace: namespace,
				Labels:    labelsSet(cluster.Name),
			},
			Type: v1.SecretTypeOpaque,
			Data: map[string][]byte{
				"username": []byte(pgUser.Name),
				"password": []byte(pgUser.Password),
			},
		}
		secrets[username] = &secret
	}

	return
}

func Service(cluster spec.ClusterName, allowedSourceRanges []string) *v1.Service {
	service := &v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labelsSet(cluster.Name),
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
			LoadBalancerSourceRanges: allowedSourceRanges,
		},
	}

	return service
}

func Endpoint(cluster spec.ClusterName) *v1.Endpoints {
	endpoints := &v1.Endpoints{
		ObjectMeta: v1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labelsSet(cluster.Name),
		},
	}

	return endpoints
}

func ThirdPartyResource(TPRName string) *extv1beta.ThirdPartyResource {
	return &extv1beta.ThirdPartyResource{
		ObjectMeta: v1.ObjectMeta{
			//ThirdPartyResources are cluster-wide
			Name: TPRName,
		},
		Versions: []extv1beta.APIVersion{
			{Name: constants.TPRApiVersion},
		},
		Description: constants.TPRDescription,
	}
}
