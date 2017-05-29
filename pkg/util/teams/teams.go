package teams

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Sirupsen/logrus"
)

type InfrastructureAccount struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
	OwnerDn     string `json:"owner_dn"`
	Disabled    bool   `json:"disabled"`
}

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

	InfrastructureAccounts []InfrastructureAccount `json:"infrastructure-accounts"`
}

type API struct {
	url                string
	httpClient         *http.Client
	logger             *logrus.Entry
	RefreshTokenAction func() (string, error)
	enabled            bool
}

func NewTeamsAPI(url string, log *logrus.Logger, enabled bool) *API {
	t := API{
		url:        strings.TrimRight(url, "/"),
		httpClient: &http.Client{},
		logger:     log.WithField("pkg", "teamsapi"),
		enabled:    enabled,
	}

	return &t
}

func (t *API) TeamInfo(teamID string) (*Team, error) {
	// TODO: avoid getting a new token on every call to the Teams API.
	if !t.enabled {
		t.logger.Debug("Team API is disabled, returning empty list of members")
		return &Team{}, nil
	}
	token, err := t.RefreshTokenAction()
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/teams/%s", t.url, teamID)
	t.logger.Debugf("Request url: %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+token)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var raw map[string]json.RawMessage
		d := json.NewDecoder(resp.Body)
		err = d.Decode(&raw)
		if err != nil {
			return nil, err
		}

		if errMessage, ok := raw["error"]; ok {
			return nil, fmt.Errorf("team API query failed with status code %d and message: '%v'", resp.StatusCode, string(errMessage))
		}

		return nil, fmt.Errorf("team API query failed with status code %d", resp.StatusCode)
	}
	teamInfo := &Team{}
	d := json.NewDecoder(resp.Body)
	err = d.Decode(teamInfo)
	if err != nil {
		return nil, err
	}

	return teamInfo, nil
}
