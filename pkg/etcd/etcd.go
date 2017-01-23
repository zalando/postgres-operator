package etcd

import (
	"fmt"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"log"
    "time"
)

const etcdKeyTemplate = "/service/%s"

type EtcdClient struct {
	apiClient client.KeysAPI
}

func NewEctdClient(host string) *EtcdClient {
    etcdClient := EtcdClient{}

    cfg := client.Config{
        Endpoints:               []string{host},
        Transport:               client.DefaultTransport,
        HeaderTimeoutPerRequest: time.Second,
    }

    c, err := client.New(cfg)
    if err != nil {
        log.Fatal(err)
    }

    etcdClient.apiClient = client.NewKeysAPI(c)

    return &etcdClient
}


func (c *EtcdClient) DeleteEtcdKey(clusterName string) error {
	options := client.DeleteOptions{
		Recursive: true,
	}

	keyName := fmt.Sprintf(etcdKeyTemplate, clusterName)

	resp, err := c.apiClient.Delete(context.Background(), keyName, &options)
	if resp != nil {
		log.Printf("Response: %+v", *resp)
	} else {
		log.Fatal("No response from etcd")
	}

	log.Printf("Deleting key %s from ETCD", clusterName)

	return err
}
