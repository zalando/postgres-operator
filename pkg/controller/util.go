package controller

import (
	"fmt"

	"github.bus.zalan.do/acid/postgres-operator/pkg/cluster"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/resources"
	"k8s.io/client-go/pkg/api"
)

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		ControllerNamespace: c.config.PodNamespace,
		KubeClient:          c.config.KubeClient,
		RestClient:          c.config.RestClient,
		EtcdClient:          c.config.EtcdClient,
		TeamsAPIClient:      c.config.TeamsAPIClient,
	}
}

func (c *Controller) getOAuthToken() (string, error) {
	// Temporary getting postgresql-operator secret from the NamespaceDefault
	credentialsSecret, err := c.config.KubeClient.Secrets(api.NamespaceDefault).Get(constants.OAuthTokenSecretName)

	if err != nil {
		return "", fmt.Errorf("Can't get credentials secret: %s", err)
	}
	data := credentialsSecret.Data

	if string(data["read-only-token-type"]) != "Bearer" {
		return "", fmt.Errorf("Wrong token type: %s", data["read-only-token-type"])
	}

	return string(data["read-only-token-secret"]), nil
}

func (c *Controller) createTPR() error {
	TPRName := fmt.Sprintf("%s.%s", constants.TPRName, constants.TPRVendor)
	tpr := resources.ThirdPartyResource(TPRName)

	_, err := c.config.KubeClient.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)
	if err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return err
		} else {
			c.logger.Infof("ThirdPartyResource '%s' is already registered", TPRName)
		}
	} else {
		c.logger.Infof("ThirdPartyResource '%s' has been registered", TPRName)
	}

	restClient := c.config.RestClient

	return k8sutil.WaitTPRReady(restClient, constants.TPRReadyWaitInterval, constants.TPRReadyWaitTimeout, c.config.PodNamespace)
}
