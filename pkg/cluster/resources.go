package cluster

import (
	"fmt"
	"strings"

	"k8s.io/client-go/pkg/api/resource"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/pkg/util/intstr"

	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
)

var createUserSQL = `DO $$
BEGIN
    SET LOCAL synchronous_commit = 'local';
    CREATE ROLE "%s" %s PASSWORD %s;
END;
$$`

func (c *Cluster) createStatefulSet() {
	meta := (*c.cluster).Metadata

	envVars := []v1.EnvVar{
		{
			Name:  "SCOPE",
			Value: meta.Name,
		},
		{
			Name:  "PGROOT",
			Value: "/home/postgres/pgdata/pgroot",
		},
		{
			Name:  "ETCD_HOST",
			Value: c.etcdHost,
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
						Name: c.credentialSecretName(superuserName),
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
						Name: c.credentialSecretName(replicationUsername),
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
  - hostssl   all all all md5`, (*c.cluster.Spec).Version, constants.PamRoleName, constants.PamRoleName),
		},
	}

	resourceList := v1.ResourceList{}

	if cpu := (*c.cluster).Spec.Resources.Cpu; cpu != "" {
		resourceList[v1.ResourceCPU] = resource.MustParse(cpu)
	}

	if memory := (*c.cluster).Spec.Resources.Memory; memory != "" {
		resourceList[v1.ResourceMemory] = resource.MustParse(memory)
	}

	container := v1.Container{
		Name:            meta.Name,
		Image:           c.dockerImage,
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
				MountPath: "/home/postgres/pgdata", //TODO: fetch from manifesto
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
			Labels:      c.labelsSet(),
			Namespace:   meta.Namespace,
			Annotations: map[string]string{"pod.alpha.kubernetes.io/initialized": "true"},
		},
		Spec: podSpec,
	}

	statefulSet := &v1beta1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      meta.Name,
			Namespace: meta.Namespace,
			Labels:    c.labelsSet(),
		},
		Spec: v1beta1.StatefulSetSpec{
			Replicas:    &c.cluster.Spec.NumberOfInstances,
			ServiceName: meta.Name,
			Template:    template,
		},
	}

	_, err := c.config.KubeClient.StatefulSets(meta.Namespace).Create(statefulSet)
	if err != nil {
		c.logger.Errorf("Can't create statefulset: %s", err)
	} else {
		c.logger.Infof("Statefulset has been created: '%s'", util.FullObjectNameFromMeta(statefulSet.ObjectMeta))
	}
}

func (c *Cluster) applySecrets() {
	var err error
	namespace := (*c.cluster).Metadata.Namespace
	for username, pgUser := range c.pgUsers {
		//Skip users with no password i.e. human users (they'll be authenticated using pam)
		if pgUser.password == "" {
			continue
		}
		secret := v1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      c.credentialSecretName(username),
				Namespace: namespace,
				Labels:    c.labelsSet(),
			},
			Type: v1.SecretTypeOpaque,
			Data: map[string][]byte{
				"username": []byte(pgUser.name),
				"password": []byte(pgUser.password),
			},
		}
		_, err = c.config.KubeClient.Secrets(namespace).Create(&secret)
		if k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			c.logger.Infof("Skipping update of '%s'", secret.Name)

			curSecrets, err := c.config.KubeClient.Secrets(namespace).Get(c.credentialSecretName(username))
			if err != nil {
				c.logger.Errorf("Can't get current secret: %s", err)
			}
			user := pgUser
			user.password = string(curSecrets.Data["password"])
			c.pgUsers[username] = user
			c.logger.Infof("Password fetched for user '%s' from the secrets", username)

			continue
		} else {
			if err != nil {
				c.logger.Errorf("Error while creating secret: %s", err)
			} else {
				c.logger.Infof("Secret created: '%s'", util.FullObjectNameFromMeta(secret.ObjectMeta))
			}
		}
	}
}

func (c *Cluster) createService() {
	meta := (*c.cluster).Metadata

	_, err := c.config.KubeClient.Services(meta.Namespace).Get(meta.Name)
	if !k8sutil.ResourceNotFound(err) {
		c.logger.Infof("Service '%s' already exists", meta.Name)
		return
	}

	service := v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      meta.Name,
			Namespace: meta.Namespace,
			Labels:    c.labelsSet(),
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
			LoadBalancerSourceRanges: (*c.cluster).Spec.AllowedSourceRanges,
		},
	}

	_, err = c.config.KubeClient.Services(meta.Namespace).Create(&service)
	if err != nil {
		c.logger.Errorf("Error while creating service: %+v", err)
	} else {
		c.logger.Infof("Service created: '%s'", util.FullObjectNameFromMeta(service.ObjectMeta))
	}
}

func (c *Cluster) createEndpoint() {
	meta := (*c.cluster).Metadata

	_, err := c.config.KubeClient.Endpoints(meta.Namespace).Get(meta.Name)
	if !k8sutil.ResourceNotFound(err) {
		c.logger.Infof("Endpoint '%s' already exists", meta.Name)
		return
	}

	endpoint := v1.Endpoints{
		ObjectMeta: v1.ObjectMeta{
			Name:      meta.Name,
			Namespace: meta.Namespace,
			Labels:    c.labelsSet(),
		},
	}

	_, err = c.config.KubeClient.Endpoints(meta.Namespace).Create(&endpoint)
	if err != nil {
		c.logger.Errorf("Error while creating endpoint: %+v", err)
	} else {
		c.logger.Infof("Endpoint created: %s", endpoint.Name)
	}
}

func (c *Cluster) createUser(user pgUser) {
	var userType string
	var flags []string = user.flags

	if user.password == "" {
		userType = "human"
		flags = append(flags, fmt.Sprintf("IN ROLE \"%s\"", constants.PamRoleName))
	} else {
		userType = "app"
	}

	addLoginFlag := true
	for _, v := range flags {
		if v == "NOLOGIN" {
			addLoginFlag = false
			break
		}
	}
	if addLoginFlag {
		flags = append(flags, "LOGIN")
	}

	userFlags := strings.Join(flags, " ")
	userPassword := fmt.Sprintf("'%s'", user.password)
	if user.password == "" {
		userPassword = "NULL"
	}
	query := fmt.Sprintf(createUserSQL, user.name, userFlags, userPassword)

	_, err := c.pgDb.Query(query)
	if err != nil {
		c.logger.Errorf("Can't create %s user '%s': %s", user.name, err)
	} else {
		c.logger.Infof("Created %s user '%s' with %s flags", userType, user.name, flags)
	}
}

func (c *Cluster) createUsers() error {
	for username, user := range c.pgUsers {
		if username == superuserName || username == replicationUsername {
			continue
		}

		c.createUser(user)
	}

	return nil
}
