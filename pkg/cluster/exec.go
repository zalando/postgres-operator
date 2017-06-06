package cluster

import (
	"bytes"
	"fmt"

	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/pkg/api"
	"k8s.io/kubernetes/pkg/client/unversioned/remotecommand"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

func (c *Cluster) ExecCommand(podName *spec.NamespacedName, command ...string) (string, error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	pod, err := c.KubeClient.Pods(podName.Namespace).Get(podName.Name)
	if err != nil {
		return "", fmt.Errorf("could not get pod info: %v", err)
	}

	if len(pod.Spec.Containers) != 1 {
		return "", fmt.Errorf("could not determine which container to use")
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
		return "", fmt.Errorf("failed to init executor: %v", err)
	}

	err = exec.Stream(remotecommand.StreamOptions{
		SupportedProtocols: remotecommandconsts.SupportedStreamingProtocols,
		Stdout:             &execOut,
		Stderr:             &execErr,
	})

	if err != nil {
		return "", fmt.Errorf("could not execute: %v", err)
	}

	if execErr.Len() > 0 {
		return "", fmt.Errorf("stderr: %v", execErr.String())
	}

	return execOut.String(), nil
}
