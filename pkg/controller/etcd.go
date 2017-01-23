package controller

import (
	"fmt"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"log"
)

func (z *SpiloSupervisor) DeleteEtcdKey(clusterName string) error {
	options := client.DeleteOptions{
		Recursive: true,
	}

	keyName := fmt.Sprintf(etcdKeyTemplate, clusterName)

	resp, err := z.etcdApiClient.Delete(context.Background(), keyName, &options)
	if resp != nil {
		log.Printf("Response: %+v", *resp)
	} else {
		log.Fatal("No response from etcd")
	}

	log.Printf("Deleting key %s from ETCD", clusterName)

	return err
}
