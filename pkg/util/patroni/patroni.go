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
	Status(pod *v1.Pod) (*Status, error)
}

// Patroni API client
type Patroni struct {
	httpClient *http.Client
	logger     *logrus.Entry
}

//XlogSpec...
type XlogSpec struct {
	Location uint64 `json:"location"`
}

//ReplicationSpec describes replication specific parameters
type ReplicationSpec struct {
	State           string `json:"state"`
	SyncPriority    uint   `json:"sync_priority"`
	SyncState       string `json:"sync_state"`
	Usename         string `json:"usename"`
	ApplicationName string `json:"application_name"`
	ClientAddr      string `json:"client_addr"`
}

// PatroniSpec describes patroni specific parameters
type PatroniSpec struct {
	Scope   string `json:"scope"`
	Version string `json:"version"`
}

// Status describes status returned by patroni API
type Status struct {
	State                    string            `json:"state"`
	Xlog                     XlogSpec          `json:"xlog"`
	ServerVersion            uint64            `json:"server_version"`
	Role                     string            `json:"role"`
	Replication              []ReplicationSpec `json:"replication"`
	DatabaseSystemIdentifier string            `json:"database_system_identifier"`
	PostmasterStartTime      string            `json:"postmaster_start_time"`
	Patroni                  PatroniSpec       `json:"patroni"`
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

// Status returns patroni status
func (p *Patroni) Status(pod *v1.Pod) (*Status, error) {
	status := &Status{}

	request, err := http.NewRequest(http.MethodGet, apiURL(pod), nil)
	if err != nil {
		return nil, fmt.Errorf("could not make new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("could not make request: %v", err)
	}

	err = json.NewDecoder(resp.Body).Decode(status)
	if err != nil {
		return nil, fmt.Errorf("could not parse response from patroni: %v", err)
	}

	return status, nil
}
