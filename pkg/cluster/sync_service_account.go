package cluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zalando/postgres-operator/pkg/util/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Cluster) syncPodServiceAccount() error {
	sa, err := c.KubeClient.ServiceAccounts(c.Namespace).Get(context.TODO(), c.OpConfig.PodServiceAccountName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get pod service account %q: %v", c.OpConfig.PodServiceAccountName, err)
	}

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]*string{},
		},
	}
	patchAnnotations := patch["metadata"].(map[string]interface{})["annotations"].(map[string]*string)

	changed := false

	if c.OpConfig.IrsaRoleARN != "" {
		if val, ok := sa.Annotations[constants.IrsaAnnotation]; !ok || val != c.OpConfig.IrsaRoleARN {
			v := c.OpConfig.IrsaRoleARN
			patchAnnotations[constants.IrsaAnnotation] = &v
			changed = true
		}
	} else {
		if _, ok := sa.Annotations[constants.IrsaAnnotation]; ok {
			patchAnnotations[constants.IrsaAnnotation] = nil
			changed = true
		}
	}

	if changed {
		patchData, err := json.Marshal(patch)
		if err != nil {
			return fmt.Errorf("could not marshal service account patch: %v", err)
		}
		if _, err = c.KubeClient.ServiceAccounts(c.Namespace).Patch(context.TODO(), sa.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("could not patch pod service account %q: %v", sa.Name, err)
		}
		c.logger.Infof("synced annotations on pod service account %q", sa.Name)
	}

	if c.OpConfig.IrsaRoleARN != "" {
		c.logIRSAMigrationProgress()
	}

	return nil
}

func (c *Cluster) logIRSAMigrationProgress() {
	pods, err := c.listPods()
	if err != nil {
		c.logger.Warnf("IRSA migration: could not list pods: %v", err)
		return
	}

	total := len(pods)
	remaining := 0
	for _, pod := range pods {
		if _, ok := pod.Annotations[constants.KubeIAmAnnotation]; ok {
			remaining++
		}
	}

	if remaining > 0 {
		c.logger.Infof("IRSA migration in progress: %d/%d pods still carry kube2iam annotation, will be removed on next rotation", remaining, total)
	} else {
		c.logger.Infof("IRSA migration complete: all %d pods have rotated, kube2iam annotation fully drained", total)
	}
}
