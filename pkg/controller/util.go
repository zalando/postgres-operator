package controller

import (
	"fmt"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	extv1beta "k8s.io/client-go/pkg/apis/extensions/v1beta1"

	"github.bus.zalan.do/acid/postgres-operator/pkg/cluster"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
)

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		KubeClient:     c.KubeClient,
		RestClient:     c.RestClient,
		EtcdClient:     c.EtcdClient,
		TeamsAPIClient: c.TeamsAPIClient,
		OpConfig:       c.opConfig,
	}
}

func (c *Controller) getOAuthToken() (string, error) {
	// Temporary getting postgresql-operator secret from the NamespaceDefault
	credentialsSecret, err := c.KubeClient.Secrets(api.NamespaceDefault).Get(c.opConfig.OAuthTokenSecretName)

	if err != nil {
		return "", fmt.Errorf("Can't get credentials Secret: %s", err)
	}
	data := credentialsSecret.Data

	if string(data["read-only-token-type"]) != "Bearer" {
		return "", fmt.Errorf("Wrong token type: %s", data["read-only-token-type"])
	}

	return string(data["read-only-token-secret"]), nil
}

func thirdPartyResource(TPRName string) *extv1beta.ThirdPartyResource {
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

func (c *Controller) createTPR() error {
	TPRName := fmt.Sprintf("%s.%s", constants.TPRName, constants.TPRVendor)
	tpr := thirdPartyResource(TPRName)

	_, err := c.KubeClient.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)
	if err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return err
		} else {
			c.logger.Infof("ThirdPartyResource '%s' is already registered", TPRName)
		}
	} else {
		c.logger.Infof("ThirdPartyResource '%s' has been registered", TPRName)
	}

	restClient := c.RestClient

	return k8sutil.WaitTPRReady(restClient, c.opConfig.TPR.ReadyWaitInterval, c.opConfig.TPR.ReadyWaitTimeout, c.PodNamespace)
}
