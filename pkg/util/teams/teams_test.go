package teams

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sirupsen/logrus"
)

var (
	logger = logrus.New().WithField("pkg", "teamsapi")
	token  = "ec45b1cfbe7100c6315d183a3eb6cec0M2U1LWJkMzEtZDgzNzNmZGQyNGM3IiwiYXV0aF90aW1lIjoxNDkzNzMwNzQ1LCJpc3MiOiJodHRwcz"
)

var teamsAPItc = []struct {
	in     string
	inCode int
	out    *Team
	err    error
}{
	{`{
"dn": "cn=100100,ou=official,ou=foobar,dc=zalando,dc=net",
"id": "acid",
"id_name": "ACID",
"team_id": "111222",
"type": "official",
"name": "Acid team name",
"mail": [
"email1@example.com",
"email2@example.com"
],
"alias": [
"acid"
],
"member": [
  "member1",
  "member2",
  "member3"
],
"infrastructure-accounts": [
{
  "id": "1234512345",
  "name": "acid",
  "provider": "aws",
  "type": "aws",
  "description": "",
  "owner": "acid",
  "owner_dn": "cn=100100,ou=official,ou=foobar,dc=zalando,dc=net",
  "disabled": false
},
{
  "id": "5432154321",
  "name": "db",
  "provider": "aws",
  "type": "aws",
  "description": "",
  "owner": "acid",
  "owner_dn": "cn=100100,ou=official,ou=foobar,dc=zalando,dc=net",
  "disabled": false
}
],
"cost_center": "00099999",
"delivery_lead": "member4",
"parent_team_id": "111221"
}`,
		200,
		&Team{
			Dn:           "cn=100100,ou=official,ou=foobar,dc=zalando,dc=net",
			ID:           "acid",
			TeamName:     "ACID",
			TeamID:       "111222",
			Type:         "official",
			FullName:     "Acid team name",
			Aliases:      []string{"acid"},
			Mails:        []string{"email1@example.com", "email2@example.com"},
			Members:      []string{"member1", "member2", "member3"},
			CostCenter:   "00099999",
			DeliveryLead: "member4",
			ParentTeamID: "111221",
			InfrastructureAccounts: []infrastructureAccount{
				{
					ID:          "1234512345",
					Name:        "acid",
					Provider:    "aws",
					Type:        "aws",
					Description: "",
					Owner:       "acid",
					OwnerDn:     "cn=100100,ou=official,ou=foobar,dc=zalando,dc=net",
					Disabled:    false},
				{
					ID:          "5432154321",
					Name:        "db",
					Provider:    "aws",
					Type:        "aws",
					Description: "",
					Owner:       "acid",
					OwnerDn:     "cn=100100,ou=official,ou=foobar,dc=zalando,dc=net",
					Disabled:    false},
			},
		},
		nil}, {
		`{"error": "Access Token not valid"}`,
		401,
		nil,
		fmt.Errorf(`team API query failed with status code 401 and message: '"Access Token not valid"'`),
	},
	{
		`{"status": "I'm a teapot'"}`,
		418,
		nil,
		fmt.Errorf(`team API query failed with status code 418`),
	},
	{
		`{"status": "I'm a teapot`,
		418,
		nil,
		fmt.Errorf(`team API query failed with status code 418 and malformed response: unexpected EOF`),
	},
	{
		`{"status": "I'm a teapot`,
		200,
		nil,
		fmt.Errorf(`could not parse team API response: unexpected EOF`),
	},
}

var requestsURLtc = []struct {
	url string
	err error
}{
	{
		"coffee://localhost/",
		fmt.Errorf(`Get coffee://localhost/teams/acid: unsupported protocol scheme "coffee"`),
	},
	{
		"http://192.168.0.%31/",
		fmt.Errorf(`parse http://192.168.0.%%31/teams/acid: invalid URL escape "%%31"`),
	},
}

func TestInfo(t *testing.T) {
	for _, tc := range teamsAPItc {
		func() {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Errorf("authorization token is wrong or not provided")
				}
				w.WriteHeader(tc.inCode)
				if _, err := fmt.Fprint(w, tc.in); err != nil {
					t.Errorf("error writing teams api response %v", err)
				}
			}))
			defer ts.Close()
			api := NewTeamsAPI(ts.URL, logger)

			actual, err := api.TeamInfo("acid", token)
			if err != nil && err.Error() != tc.err.Error() {
				t.Errorf("expected error: %v, got: %v", tc.err, err)
				return
			}

			if !reflect.DeepEqual(actual, tc.out) {
				t.Errorf("expected %#v, got: %#v", tc.out, actual)
			}
		}()
	}
}

type mockHTTPClient struct {
}

type mockBody struct {
}

func (b *mockBody) Read(p []byte) (n int, err error) {
	return 2, nil
}

func (b *mockBody) Close() error {
	return fmt.Errorf("close error")
}

func (c *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	resp := http.Response{
		Status:        "200 OK",
		StatusCode:    200,
		ContentLength: 2,
		Close:         false,
		Request:       req,
	}
	resp.Body = &mockBody{}

	return &resp, nil
}

func TestHttpClientClose(t *testing.T) {
	ts := httptest.NewServer(nil)

	api := NewTeamsAPI(ts.URL, logger)
	api.httpClient = &mockHTTPClient{}

	_, err := api.TeamInfo("acid", token)
	expError := fmt.Errorf("error when closing response: close error")
	if err.Error() != expError.Error() {
		t.Errorf("expected error: %v, got: %v", expError, err)
	}
}

func TestRequest(t *testing.T) {
	for _, tc := range requestsURLtc {
		api := NewTeamsAPI(tc.url, logger)
		resp, err := api.TeamInfo("acid", token)
		if resp != nil {
			t.Errorf("response expected to be nil")
			continue
		}

		if err.Error() != tc.err.Error() {
			t.Errorf("expected error: %v, got: %v", tc.err, err)
		}
	}
}
