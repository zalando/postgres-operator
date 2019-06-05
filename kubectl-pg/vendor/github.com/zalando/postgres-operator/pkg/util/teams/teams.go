package teams

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

// InfrastructureAccount defines an account of the team on some infrastructure (i.e AWS, Google) platform.
type infrastructureAccount struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
	OwnerDn     string `json:"owner_dn"`
	Disabled    bool   `json:"disabled"`
}

// Team defines informaiton for a single team, including the list of members and infrastructure accounts.
type Team struct {
	Dn           string   `json:"dn"`
	ID           string   `json:"id"`
	TeamName     string   `json:"id_name"`
	TeamID       string   `json:"team_id"`
	Type         string   `json:"type"`
	FullName     string   `json:"name"`
	Aliases      []string `json:"alias"`
	Mails        []string `json:"mail"`
	Members      []string `json:"member"`
	CostCenter   string   `json:"cost_center"`
	DeliveryLead string   `json:"delivery_lead"`
	ParentTeamID string   `json:"parent_team_id"`

	InfrastructureAccounts []infrastructureAccount `json:"infrastructure-accounts"`
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Interface to the TeamsAPIClient
type Interface interface {
	TeamInfo(teamID, token string) (tm *Team, err error)
}

// API describes teams API
type API struct {
	httpClient
	url    string
	logger *logrus.Entry
}

// NewTeamsAPI creates an object to query the team API.
func NewTeamsAPI(url string, log *logrus.Entry) *API {
	t := API{
		url:        strings.TrimRight(url, "/"),
		httpClient: &http.Client{},
		logger:     log.WithField("pkg", "teamsapi"),
	}

	return &t
}

// TeamInfo returns information about a given team using its ID and a token to authenticate to the API service.
func (t *API) TeamInfo(teamID, token string) (tm *Team, err error) {
	var (
		req  *http.Request
		resp *http.Response
	)

	url := fmt.Sprintf("%s/teams/%s", t.url, teamID)
	t.logger.Debugf("request url: %s", url)
	req, err = http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+token)
	if resp, err = t.httpClient.Do(req); err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = fmt.Errorf("error when closing response: %v", closeErr)
		}
	}()
	if resp.StatusCode != 200 {
		var raw map[string]json.RawMessage
		d := json.NewDecoder(resp.Body)
		err = d.Decode(&raw)
		if err != nil {
			return nil, fmt.Errorf("team API query failed with status code %d and malformed response: %v", resp.StatusCode, err)
		}

		if errMessage, ok := raw["error"]; ok {
			return nil, fmt.Errorf("team API query failed with status code %d and message: '%v'", resp.StatusCode, string(errMessage))
		}
		return nil, fmt.Errorf("team API query failed with status code %d", resp.StatusCode)
	}

	tm = &Team{}
	d := json.NewDecoder(resp.Body)
	if err = d.Decode(tm); err != nil {
		return nil, fmt.Errorf("could not parse team API response: %v", err)
	}

	return tm, nil
}
