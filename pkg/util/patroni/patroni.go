package patroni

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/zalando/postgres-operator/pkg/util/constants"
	httpclient "github.com/zalando/postgres-operator/pkg/util/httpclient"

	"github.com/sirupsen/logrus"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	v1 "k8s.io/api/core/v1"
)

const (
	failoverPath = "/failover"
	configPath   = "/config"
	clusterPath  = "/cluster"
	statusPath   = "/patroni"
	restartPath  = "/restart"
	ApiPort      = 8008
	timeout      = 30 * time.Second
)

// Interface describe patroni methods
type Interface interface {
	GetClusterMembers(master *v1.Pod) ([]ClusterMember, error)
	Switchover(master *v1.Pod, candidate string) error
	SetPostgresParameters(server *v1.Pod, options map[string]string) error
	GetMemberData(server *v1.Pod) (MemberData, error)
	Restart(server *v1.Pod) error
	GetConfig(server *v1.Pod) (acidv1.Patroni, map[string]string, error)
	SetConfig(server *v1.Pod, config map[string]interface{}) error
}

// Patroni API client
type Patroni struct {
	httpClient httpclient.HTTPClient
	logger     *logrus.Entry
}

// New create patroni
func New(logger *logrus.Entry, client httpclient.HTTPClient) *Patroni {
	if client == nil {

		client = &http.Client{
			Timeout: timeout,
		}

	}

	return &Patroni{
		logger:     logger,
		httpClient: client,
	}
}

func apiURL(masterPod *v1.Pod) (string, error) {
	ip := net.ParseIP(masterPod.Status.PodIP)
	if ip == nil {
		return "", fmt.Errorf("%s is not a valid IP", masterPod.Status.PodIP)
	}
	// Sanity check PodIP
	if ip.To4() == nil {
		if ip.To16() == nil {
			// Shouldn't ever get here, but library states it's possible.
			return "", fmt.Errorf("%s is not a valid IPv4/IPv6 address", masterPod.Status.PodIP)
		}
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(ip.String(), strconv.Itoa(ApiPort))), nil
}

func (p *Patroni) httpPostOrPatch(method string, url string, body *bytes.Buffer) (err error) {
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return fmt.Errorf("could not create request: %v", err)
	}

	if p.logger != nil {
		p.logger.Debugf("making %s http request: %s", method, request.URL.String())
	}

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

func (p *Patroni) httpGet(url string) (string, error) {
	p.logger.Debugf("making GET http request: %s", url)

	response, err := p.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("could not make request: %v", err)
	}
	defer response.Body.Close()

	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("could not read response: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		return string(bodyBytes), fmt.Errorf("patroni returned '%d'", response.StatusCode)
	}

	return string(bodyBytes), nil
}

// Switchover by calling Patroni REST API
func (p *Patroni) Switchover(master *v1.Pod, candidate string) error {
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(map[string]string{"leader": master.Name, "member": candidate})
	if err != nil {
		return fmt.Errorf("could not encode json: %v", err)
	}
	apiURLString, err := apiURL(master)
	if err != nil {
		return err
	}
	return p.httpPostOrPatch(http.MethodPost, apiURLString+failoverPath, buf)
}

//TODO: add an option call /patroni to check if it is necessary to restart the server

//SetPostgresParameters sets Postgres options via Patroni patch API call.
func (p *Patroni) SetPostgresParameters(server *v1.Pod, parameters map[string]string) error {
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(map[string]map[string]interface{}{"postgresql": {"parameters": parameters}})
	if err != nil {
		return fmt.Errorf("could not encode json: %v", err)
	}
	apiURLString, err := apiURL(server)
	if err != nil {
		return err
	}
	return p.httpPostOrPatch(http.MethodPatch, apiURLString+configPath, buf)
}

//SetConfig sets Patroni options via Patroni patch API call.
func (p *Patroni) SetConfig(server *v1.Pod, config map[string]interface{}) error {
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(config)
	if err != nil {
		return fmt.Errorf("could not encode json: %v", err)
	}
	apiURLString, err := apiURL(server)
	if err != nil {
		return err
	}
	return p.httpPostOrPatch(http.MethodPatch, apiURLString+configPath, buf)
}

// ClusterMembers array of cluster members from Patroni API
type ClusterMembers struct {
	Members []ClusterMember `json:"members"`
}

// ClusterMember cluster member data from Patroni API
type ClusterMember struct {
	Name     string         `json:"name"`
	Role     string         `json:"role"`
	State    string         `json:"state"`
	Timeline int            `json:"timeline"`
	Lag      ReplicationLag `json:"lag,omitempty"`
}

type ReplicationLag uint64

// UnmarshalJSON converts member lag (can be int or string) into uint64
func (rl *ReplicationLag) UnmarshalJSON(data []byte) error {
	var lagUInt64 uint64
	if data[0] == '"' {
		*rl = math.MaxUint64
		return nil
	}
	if err := json.Unmarshal(data, &lagUInt64); err != nil {
		return err
	}
	*rl = ReplicationLag(lagUInt64)
	return nil
}

// MemberDataPatroni child element
type MemberDataPatroni struct {
	Version string `json:"version"`
	Scope   string `json:"scope"`
}

// MemberData Patroni member data from Patroni API
type MemberData struct {
	State           string            `json:"state"`
	Role            string            `json:"role"`
	ServerVersion   int               `json:"server_version"`
	PendingRestart  bool              `json:"pending_restart"`
	ClusterUnlocked bool              `json:"cluster_unlocked"`
	Patroni         MemberDataPatroni `json:"patroni"`
}

func (p *Patroni) GetConfig(server *v1.Pod) (acidv1.Patroni, map[string]string, error) {
	var (
		patroniConfig acidv1.Patroni
		pgConfig      map[string]interface{}
	)
	apiURLString, err := apiURL(server)
	if err != nil {
		return patroniConfig, nil, err
	}
	body, err := p.httpGet(apiURLString + configPath)
	if err != nil {
		return patroniConfig, nil, err
	}
	err = json.Unmarshal([]byte(body), &patroniConfig)
	if err != nil {
		return patroniConfig, nil, err
	}

	// unmarshalling postgresql parameters needs a detour
	err = json.Unmarshal([]byte(body), &pgConfig)
	if err != nil {
		return patroniConfig, nil, err
	}
	pgParameters := make(map[string]string)
	if _, exists := pgConfig["postgresql"]; exists {
		effectivePostgresql := pgConfig["postgresql"].(map[string]interface{})
		effectivePgParameters := effectivePostgresql[constants.PatroniPGParametersParameterName].(map[string]interface{})
		for parameter, value := range effectivePgParameters {
			strValue := fmt.Sprintf("%v", value)
			pgParameters[parameter] = strValue
		}
	}

	return patroniConfig, pgParameters, err
}

// Restart method restarts instance via Patroni POST API call.
func (p *Patroni) Restart(server *v1.Pod) error {
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(map[string]interface{}{"restart_pending": true})
	if err != nil {
		return fmt.Errorf("could not encode json: %v", err)
	}
	apiURLString, err := apiURL(server)
	if err != nil {
		return err
	}
	if err := p.httpPostOrPatch(http.MethodPost, apiURLString+restartPath, buf); err != nil {
		return err
	}
	p.logger.Infof("Postgres server successfuly restarted in pod %s", server.Name)

	return nil
}

// GetClusterMembers read cluster data from patroni API
func (p *Patroni) GetClusterMembers(server *v1.Pod) ([]ClusterMember, error) {

	apiURLString, err := apiURL(server)
	if err != nil {
		return []ClusterMember{}, err
	}
	body, err := p.httpGet(apiURLString + clusterPath)
	if err != nil {
		return []ClusterMember{}, err
	}

	data := ClusterMembers{}
	err = json.Unmarshal([]byte(body), &data)
	if err != nil {
		return []ClusterMember{}, err
	}

	return data.Members, nil
}

// GetMemberData read member data from patroni API
func (p *Patroni) GetMemberData(server *v1.Pod) (MemberData, error) {

	apiURLString, err := apiURL(server)
	if err != nil {
		return MemberData{}, err
	}
	body, err := p.httpGet(apiURLString + statusPath)
	if err != nil {
		return MemberData{}, err
	}

	data := MemberData{}
	err = json.Unmarshal([]byte(body), &data)
	if err != nil {
		return MemberData{}, err
	}

	return data, nil
}
