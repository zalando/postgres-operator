package controller

import (
	"bytes"
	"fmt"

	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/pkg/api"
	"k8s.io/kubernetes/pkg/client/unversioned/remotecommand"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

func (c *Controller) ExecCommand(podName spec.NamespacedName, command []string) (string, error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	pod, err := c.KubeClient.Pods(podName.Namespace).Get(podName.Name)
	if err != nil {
		return "", fmt.Errorf("Can't get Pod info: %s", err)
	}

	if len(pod.Spec.Containers) != 1 {
		return "", fmt.Errorf("Can't determine which container to use")
	}

	req := c.RestClient.Post().
		Resource("pods").
		Name(podName.Name).
		Namespace(podName.Namespace).
		SubResource("exec")
	req.VersionedParams(&api.PodExecOptions{
		Container: pod.Spec.Containers[0].Name,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, api.ParameterCodec)

	exec, err := remotecommand.NewExecutor(c.RestConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("Failed to init executor: %s", err)
	}

	err = exec.Stream(remotecommand.StreamOptions{
		SupportedProtocols: remotecommandconsts.SupportedStreamingProtocols,
		Stdout:             &execOut,
		Stderr:             &execErr,
	})

	if err != nil {
		return "", fmt.Errorf("Can't execute: %s", err)
	}

	if execErr.Len() > 0 {
		return "", fmt.Errorf("Stderr: %s", execErr.String())
	}

	return execOut.String(), nil
}
