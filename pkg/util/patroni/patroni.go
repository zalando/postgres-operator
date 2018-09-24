package patroni

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
)

const (
	failoverPath = "/failover"
	configPath   = "/config"
	apiPort      = 8008
	timeout      = 30 * time.Second
)

// Interface describe patroni methods
type Interface interface {
	Switchover(master *v1.Pod, candidate string) error
	SetPostgresParameters(server *v1.Pod, options map[string]string) error
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

func (p *Patroni) httpPostOrPatch(method string, url string, body *bytes.Buffer) (err error) {
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return fmt.Errorf("could not create request: %v", err)
	}

	p.logger.Debugf("making %s http request: %s", method, request.URL.String())

	resp, err := p.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("could not make request: %v", err)
	}
	defer func() {
		if err2 := resp.Body.Close(); err2 != nil {
			if err != nil {
				err = fmt.Errorf("could not close request: %v, prior error: %v", err2, err)
			} else {
				err = fmt.Errorf("could not close request: %v", err2)
			}
			return
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("could not read response: %v", err)
		}

		return fmt.Errorf("patroni returned '%s'", string(bodyBytes))
	}
	return nil
}

// Switchover by calling Patroni REST API
func (p *Patroni) Switchover(master *v1.Pod, candidate string) error {
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(map[string]string{"leader": master.Name, "member": candidate})
	if err != nil {
		return fmt.Errorf("could not encode json: %v", err)
	}
	return p.httpPostOrPatch(http.MethodPost, apiURL(master)+failoverPath, buf)
}

//TODO: add an option call /patroni to check if it is necessary to restart the server

//SetPostgresParameters sets Postgres options via Patroni patch API call.
func (p *Patroni) SetPostgresParameters(server *v1.Pod, parameters map[string]string) error {
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(map[string]map[string]interface{}{"postgresql": {"parameters": parameters}})
	if err != nil {
		return fmt.Errorf("could not encode json: %v", err)
	}
	return p.httpPostOrPatch(http.MethodPatch, apiURL(server)+configPath, buf)
}
