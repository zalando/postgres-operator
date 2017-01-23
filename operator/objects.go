package operator

import (
	"k8s.io/client-go/pkg/api/resource"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/pkg/util/intstr"
	"log"
)

func (z *SpiloSupervisor) CreateStatefulSet(spilo *Spilo) {
	ns := (*spilo).Metadata.Namespace

	statefulSet := z.createSetFromSpilo(spilo)

	_, err := z.Clientset.StatefulSets(ns).Create(&statefulSet)
	if err != nil {
		log.Printf("Petset error: %+v", err)
	} else {
		log.Printf("Petset created: %+v", statefulSet)
	}
}

func (z *SpiloSupervisor) createSetFromSpilo(spilo *Spilo) v1beta1.StatefulSet {
	clusterName := (*spilo).Metadata.Name

	envVars := []v1.EnvVar{
		{
			Name:  "SCOPE",
			Value: clusterName,
		},
		{
			Name:  "PGROOT",
			Value: "/home/postgres/pgdata/pgroot",
		},
		{
			Name:  "ETCD_HOST",
			Value: spilo.Spec.EtcdHost,
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
						Name: clusterName,
					},
					Key: "superuser-password",
				},
			},
		},
		{
			Name: "PGPASSWORD_ADMIN",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: clusterName,
					},
					Key: "admin-password",
				},
			},
		},
		{
			Name: "PGPASSWORD_STANDBY",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: clusterName,
					},
					Key: "replication-password",
				},
			},
		},
	}

	resourceList := v1.ResourceList{}

	if (*spilo).Spec.ResourceCPU != "" {
		resourceList[v1.ResourceCPU] = resource.MustParse((*spilo).Spec.ResourceCPU)
	}

	if (*spilo).Spec.ResourceMemory != "" {
		resourceList[v1.ResourceMemory] = resource.MustParse((*spilo).Spec.ResourceMemory)
	}

	container := v1.Container{
		Name:            clusterName,
		Image:           spilo.Spec.DockerImage,
		ImagePullPolicy: v1.PullAlways,
		Resources: v1.ResourceRequirements{
			Requests: resourceList,
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
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "pgdata",
				MountPath: "/home/postgres/pgdata",
			},
		},
		Env: envVars,
	}

	terminateGracePeriodSeconds := int64(30)

	podSpec := v1.PodSpec{
		TerminationGracePeriodSeconds: &terminateGracePeriodSeconds,
		Volumes: []v1.Volume{
			{
				Name:         "pgdata",
				VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}},
			},
		},
		Containers: []v1.Container{container},
	}

	template := v1.PodTemplateSpec{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"application":   "spilo",
				"spilo-cluster": clusterName,
			},
			Annotations: map[string]string{"pod.alpha.kubernetes.io/initialized": "true"},
		},
		Spec: podSpec,
	}

	return v1beta1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name: clusterName,
			Labels: map[string]string{
				"application":   "spilo",
				"spilo-cluster": clusterName,
			},
		},
		Spec: v1beta1.StatefulSetSpec{
			Replicas:    &spilo.Spec.NumberOfInstances,
			ServiceName: clusterName,
			Template:    template,
		},
	}
}

func (z *SpiloSupervisor) CreateSecrets(ns, name string) {
	secret := v1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"application":   "spilo",
				"spilo-cluster": name,
			},
		},
		Type: v1.SecretTypeOpaque,
		Data: map[string][]byte{
			"superuser-password":   []byte("emFsYW5kbw=="),
			"replication-password": []byte("cmVwLXBhc3M="),
			"admin-password":       []byte("YWRtaW4="),
		},
	}

	_, err := z.Clientset.Secrets(ns).Create(&secret)
	if err != nil {
		log.Printf("Secret error: %+v", err)
	} else {
		log.Printf("Secret created: %+v", secret)
	}
}

func (z *SpiloSupervisor) CreateService(ns, name string) {
	service := v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"application":   "spilo",
				"spilo-cluster": name,
			},
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{{Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
		},
	}

	_, err := z.Clientset.Services(ns).Create(&service)
	if err != nil {
		log.Printf("Service error: %+v", err)
	} else {
		log.Printf("Service created: %+v", service)
	}
}

func (z *SpiloSupervisor) CreateEndPoint(ns, name string) {
	endPoint := v1.Endpoints{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"application":   "spilo",
				"spilo-cluster": name,
			},
		},
	}

	_, err := z.Clientset.Endpoints(ns).Create(&endPoint)
	if err != nil {
		log.Printf("Endpoint error: %+v", err)
	} else {
		log.Printf("Endpoint created: %+v", endPoint)
	}
}
