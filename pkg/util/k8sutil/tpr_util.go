package k8sutil

import (
	"fmt"
	"time"

	"k8s.io/client-go/rest"

	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/retryutil"
)

func listClustersURI(ns string) string {
	return fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s", constants.TPRVendor, constants.TPRApiVersion, ns, constants.ResourceName)
}

func WaitTPRReady(restclient rest.Interface, interval, timeout time.Duration, ns string) error {
	return retryutil.Retry(interval, int(timeout/interval), func() (bool, error) {
		_, err := restclient.Get().RequestURI(listClustersURI(ns)).DoRaw()
		if err != nil {
			if ResourceNotFound(err) { // not set up yet. wait more.
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}
