package cluster

import (
	"bytes"
	"fmt"
	"strings"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/constants"
)

//ExecCommand executes arbitrary command inside the pod
func (c *Cluster) ExecCommand(podName *spec.NamespacedName, command ...string) (string, error) {
	c.setProcessName("executing command %q", strings.Join(command, " "))

	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	pod, err := c.KubeClient.Pods(podName.Namespace).Get(podName.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("could not get pod info: %v", err)
	}

	// iterate through all containers looking for the one running PostgreSQL.
	targetContainer := -1
	for i, cr := range pod.Spec.Containers {
		if cr.Name == constants.PostgresContainerName {
			targetContainer = i
			break
		}
	}

	if targetContainer < 0 {
		return "", fmt.Errorf("could not find %s container to exec to", constants.PostgresContainerName)
	}

	req := c.KubeClient.RESTClient.Post().
		Resource("pods").
		Name(podName.Name).
		Namespace(podName.Namespace).
		SubResource("exec")
	req.VersionedParams(&v1.PodExecOptions{
		Container: pod.Spec.Containers[targetContainer].Name,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.RestConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to init executor: %v", err)
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})

	if err != nil {
		return "", fmt.Errorf("could not execute: %v", err)
	}

	if execErr.Len() > 0 {
		return "", fmt.Errorf("stderr: %v", execErr.String())
	}

	return execOut.String(), nil
}
