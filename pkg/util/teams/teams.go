package teams

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type InfrastructureAccount struct {
	Id          string `json:"id"`
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
	Id           string   `json:"id"`
	TeamName     string   `json:"id_name"`
	TeamId       string   `json:"team_id"`
	Type         string   `json:"official"`
	FullName     string   `json:"name"`
	Aliases      []string `json:"alias"`
	Mails        []string `json:"mail"`
	Members      []string `json:"member"`
	CostCenter   string   `json:"cost_center"`
	DeliveryLead string   `json:"delivery_lead"`
	ParentTeamId string   `json:"parent_team_id"`

	InfrastructureAccounts []InfrastructureAccount `json:"infrastructure-accounts"`
}

type TeamsAPI struct {
	url        string
	httpClient *http.Client
	OAuthToken string
}

func NewTeamsAPI(url string) *TeamsAPI {
	t := TeamsAPI{
		url:        strings.TrimRight(url, "/"),
		httpClient: &http.Client{},
	}

	return &t
}

func (t *TeamsAPI) TeamInfo(teamId string) (*Team, error) {
	url := fmt.Sprintf("%s/teams/%s", t.url, teamId)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+t.OAuthToken)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	teamInfo := &Team{}
	d := json.NewDecoder(resp.Body)
	err = d.Decode(teamInfo)
	if err != nil {
		return nil, err
	}

	return teamInfo, nil
}
