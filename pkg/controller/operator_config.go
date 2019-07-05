package controller

import (
	"fmt"

	"time"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Controller) readOperatorConfigurationFromCRD(configObjectNamespace, configObjectName string) (*acidv1.OperatorConfiguration, error) {

	config, err := c.KubeClient.AcidV1ClientSet.AcidV1().OperatorConfigurations(configObjectNamespace).Get(configObjectName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get operator configuration object %q: %v", configObjectName, err)
	}

	return config, nil
}

// importConfigurationFromCRD is a transitional function that converts CRD configuration to the one based on the configmap
func (c *Controller) importConfigurationFromCRD(fromCRD *acidv1.OperatorConfigurationData) *config.Config {
	result := &config.Config{}

	// general config
	result.EtcdHost = fromCRD.EtcdHost
	result.DockerImage = fromCRD.DockerImage
	result.Workers = fromCRD.Workers
	result.MinInstances = fromCRD.MinInstances
	result.MaxInstances = fromCRD.MaxInstances
	result.ResyncPeriod = time.Duration(fromCRD.ResyncPeriod)
	result.RepairPeriod = time.Duration(fromCRD.RepairPeriod)
	result.SetMemoryRequestToLimit = fromCRD.SetMemoryRequestToLimit
	result.ShmVolume = fromCRD.ShmVolume
	result.Sidecars = fromCRD.Sidecars

	// user config
	result.SuperUsername = fromCRD.PostgresUsersConfiguration.SuperUsername
	result.ReplicationUsername = fromCRD.PostgresUsersConfiguration.ReplicationUsername

	// kubernetes config
	result.PodServiceAccountName = fromCRD.Kubernetes.PodServiceAccountName
	result.PodServiceAccountDefinition = fromCRD.Kubernetes.PodServiceAccountDefinition
	result.PodServiceAccountRoleBindingDefinition = fromCRD.Kubernetes.PodServiceAccountRoleBindingDefinition
	result.PodEnvironmentConfigMap = fromCRD.Kubernetes.PodEnvironmentConfigMap
	result.PodTerminateGracePeriod = time.Duration(fromCRD.Kubernetes.PodTerminateGracePeriod)
	result.SpiloPrivileged = fromCRD.Kubernetes.SpiloPrivileged
	result.SpiloFSGroup = fromCRD.Kubernetes.SpiloFSGroup
	result.ClusterDomain = fromCRD.Kubernetes.ClusterDomain
	result.WatchedNamespace = fromCRD.Kubernetes.WatchedNamespace
	result.PDBNameFormat = fromCRD.Kubernetes.PDBNameFormat
	result.EnablePodDisruptionBudget = fromCRD.Kubernetes.EnablePodDisruptionBudget
	result.SecretNameTemplate = fromCRD.Kubernetes.SecretNameTemplate
	result.OAuthTokenSecretName = fromCRD.Kubernetes.OAuthTokenSecretName
	result.InfrastructureRolesSecretName = fromCRD.Kubernetes.InfrastructureRolesSecretName
	result.PodRoleLabel = fromCRD.Kubernetes.PodRoleLabel
	result.ClusterLabels = fromCRD.Kubernetes.ClusterLabels
	result.InheritedLabels = fromCRD.Kubernetes.InheritedLabels
	result.ClusterNameLabel = fromCRD.Kubernetes.ClusterNameLabel
	result.NodeReadinessLabel = fromCRD.Kubernetes.NodeReadinessLabel
	result.PodPriorityClassName = fromCRD.Kubernetes.PodPriorityClassName
	result.PodManagementPolicy = fromCRD.Kubernetes.PodManagementPolicy
	result.MasterPodMoveTimeout = fromCRD.Kubernetes.MasterPodMoveTimeout
	result.EnablePodAntiAffinity = fromCRD.Kubernetes.EnablePodAntiAffinity
	result.PodAntiAffinityTopologyKey = fromCRD.Kubernetes.PodAntiAffinityTopologyKey

	// Postgres Pod resources
	result.DefaultCPURequest = fromCRD.PostgresPodResources.DefaultCPURequest
	result.DefaultMemoryRequest = fromCRD.PostgresPodResources.DefaultMemoryRequest
	result.DefaultCPULimit = fromCRD.PostgresPodResources.DefaultCPULimit
	result.DefaultMemoryLimit = fromCRD.PostgresPodResources.DefaultMemoryLimit

	// timeout config
	result.ResourceCheckInterval = time.Duration(fromCRD.Timeouts.ResourceCheckInterval)
	result.ResourceCheckTimeout = time.Duration(fromCRD.Timeouts.ResourceCheckTimeout)
	result.PodLabelWaitTimeout = time.Duration(fromCRD.Timeouts.PodLabelWaitTimeout)
	result.PodDeletionWaitTimeout = time.Duration(fromCRD.Timeouts.PodDeletionWaitTimeout)
	result.ReadyWaitInterval = time.Duration(fromCRD.Timeouts.ReadyWaitInterval)
	result.ReadyWaitTimeout = time.Duration(fromCRD.Timeouts.ReadyWaitTimeout)

	// load balancer config
	result.DbHostedZone = fromCRD.LoadBalancer.DbHostedZone
	result.EnableMasterLoadBalancer = fromCRD.LoadBalancer.EnableMasterLoadBalancer
	result.EnableReplicaLoadBalancer = fromCRD.LoadBalancer.EnableReplicaLoadBalancer
	result.CustomServiceAnnotations = fromCRD.LoadBalancer.CustomServiceAnnotations
	result.MasterDNSNameFormat = fromCRD.LoadBalancer.MasterDNSNameFormat
	result.ReplicaDNSNameFormat = fromCRD.LoadBalancer.ReplicaDNSNameFormat

	// AWS or GCP config
	result.WALES3Bucket = fromCRD.AWSGCP.WALES3Bucket
	result.AWSRegion = fromCRD.AWSGCP.AWSRegion
	result.LogS3Bucket = fromCRD.AWSGCP.LogS3Bucket
	result.KubeIAMRole = fromCRD.AWSGCP.KubeIAMRole
	result.AdditionalSecretMount = fromCRD.AWSGCP.AdditionalSecretMount
	result.AdditionalSecretMountPath = fromCRD.AWSGCP.AdditionalSecretMountPath

	// logical backup config
	result.LogicalBackupSchedule = fromCRD.LogicalBackup.Schedule
	result.LogicalBackupDockerImage = fromCRD.LogicalBackup.DockerImage
	result.LogicalBackupS3Bucket = fromCRD.LogicalBackup.S3Bucket

	// debug config
	result.DebugLogging = fromCRD.OperatorDebug.DebugLogging
	result.EnableDBAccess = fromCRD.OperatorDebug.EnableDBAccess

	// Teams API config
	result.EnableTeamsAPI = fromCRD.TeamsAPI.EnableTeamsAPI
	result.TeamsAPIUrl = fromCRD.TeamsAPI.TeamsAPIUrl
	result.TeamAPIRoleConfiguration = fromCRD.TeamsAPI.TeamAPIRoleConfiguration
	result.EnableTeamSuperuser = fromCRD.TeamsAPI.EnableTeamSuperuser
	result.EnableAdminRoleForUsers = fromCRD.TeamsAPI.EnableAdminRoleForUsers
	result.TeamAdminRole = fromCRD.TeamsAPI.TeamAdminRole
	result.PamRoleName = fromCRD.TeamsAPI.PamRoleName
	result.PamConfiguration = fromCRD.TeamsAPI.PamConfiguration
	result.ProtectedRoles = fromCRD.TeamsAPI.ProtectedRoles
	result.PostgresSuperuserTeams = fromCRD.TeamsAPI.PostgresSuperuserTeams

	// logging REST API config
	result.APIPort = fromCRD.LoggingRESTAPI.APIPort
	result.RingLogLines = fromCRD.LoggingRESTAPI.RingLogLines
	result.ClusterHistoryEntries = fromCRD.LoggingRESTAPI.ClusterHistoryEntries

	// Scalyr config
	result.ScalyrAPIKey = fromCRD.Scalyr.ScalyrAPIKey
	result.ScalyrImage = fromCRD.Scalyr.ScalyrImage
	result.ScalyrServerURL = fromCRD.Scalyr.ScalyrServerURL
	result.ScalyrCPURequest = fromCRD.Scalyr.ScalyrCPURequest
	result.ScalyrMemoryRequest = fromCRD.Scalyr.ScalyrMemoryRequest
	result.ScalyrCPULimit = fromCRD.Scalyr.ScalyrCPULimit
	result.ScalyrMemoryLimit = fromCRD.Scalyr.ScalyrMemoryLimit

	return result
}
