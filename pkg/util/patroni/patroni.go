package patroni

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	failoverPath = "/failover"
	apiPort      = 8008
	timeout      = 30 * time.Second
)

// Interface describe patroni methods
type Interface interface {
	Failover(master *v1.Pod, candidate string) error
}

// Patroni API client
type Patroni struct {
	httpClient *http.Client
	logger     *logrus.Entry
}

// New create patroni
func New(logger *logrus.Entry) *Patroni {
	cl := http.Client{
		Timeout: timeout,
	}

	return &Patroni{
		logger:     logger,
		httpClient: &cl,
	}
}

func apiURL(masterPod *v1.Pod) string {
	return fmt.Sprintf("http://%s:%d", masterPod.Status.PodIP, apiPort)
}

// Failover does manual failover via patroni api
func (p *Patroni) Failover(master *v1.Pod, candidate string) error {
	buf := &bytes.Buffer{}

	err := json.NewEncoder(buf).Encode(map[string]string{"leader": master.Name, "member": candidate})
	if err != nil {
		return fmt.Errorf("could not encode json: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, apiURL(master)+failoverPath, buf)
	if err != nil {
		return fmt.Errorf("could not create request: %v", err)
	}

	p.logger.Debugf("making http request: %s", request.URL.String())

	resp, err := p.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("could not make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("could not read response: %v", err)
		}

		return fmt.Errorf("patroni returned '%s'", string(bodyBytes))
	}

	return nil
}
