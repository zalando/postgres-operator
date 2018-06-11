package controller

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"

	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
)


func (c *Controller) readOperatorConfigurationFromCRD(configObjectName string) (*config.OperatorConfiguration, error){
	var (
		config config.OperatorConfiguration
	)

	req := c.KubeClient.CRDREST.Get().
		Name(configObjectName).
		Namespace(c.opConfig.WatchedNamespace).
		Resource(constants.OperatorConfigCRDResource).
		VersionedParams(&metav1.ListOptions{ResourceVersion: "0"}, metav1.ParameterCodec)

	data, err := req.DoRaw();
	if err != nil {
		return nil, fmt.Errorf("could not get operator configuration object %s: %v", configObjectName, err)
	}
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("could not unmarshal operator configuration object %s, %v", configObjectName, err)
	}

	return &config, nil
}