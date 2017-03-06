package controller

import (
	"fmt"
	"time"

	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	etcdclient "github.com/coreos/etcd/client"
)

func (c *Controller) initEtcdClient() error {
	etcdUrl := fmt.Sprintf("http://%s", constants.EtcdHost)

	cfg, err := etcdclient.New(etcdclient.Config{
		Endpoints:               []string{etcdUrl},
		Transport:               etcdclient.DefaultTransport,
		HeaderTimeoutPerRequest: time.Second,
	})
	if err != nil {
		return err
	}

	c.config.EtcdClient = etcdclient.NewKeysAPI(cfg)

	return nil
}
