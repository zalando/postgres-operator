package cluster

import (
	"testing"

	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/client-go/kubernetes/fake"
)

func TestFakeClient(t *testing.T) {
	clientSet := fake.NewSimpleClientset()
	namespace := "default"

	l := labels.Set(map[string]string{
		"application": "spilo",
	})

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deployment1",
			Namespace: namespace,
			Labels:    l,
		},
	}

	clientSet.AppsV1().Deployments(namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})

	deployment2, _ := clientSet.AppsV1().Deployments(namespace).Get(context.TODO(), "my-deployment1", metav1.GetOptions{})

	if deployment.ObjectMeta.Name != deployment2.ObjectMeta.Name {
		t.Errorf("Deployments are not equal")
	}

	deployments, _ := clientSet.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "application=spilo"})

	if len(deployments.Items) != 1 {
		t.Errorf("Label search does not work")
	}
}
