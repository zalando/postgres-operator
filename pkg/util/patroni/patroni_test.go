package patroni

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/zalando/postgres-operator/mocks"

	v1 "k8s.io/api/core/v1"
)

func newMockPod(ip string) *v1.Pod {
	return &v1.Pod{
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}

func TestApiURL(t *testing.T) {
	var testTable = []struct {
		podIP            string
		expectedResponse string
		expectedError    error
	}{
		{
			"127.0.0.1",
			fmt.Sprintf("http://127.0.0.1:%d", apiPort),
			nil,
		},
		{
			"0000:0000:0000:0000:0000:0000:0000:0001",
			fmt.Sprintf("http://[::1]:%d", apiPort),
			nil,
		},
		{
			"::1",
			fmt.Sprintf("http://[::1]:%d", apiPort),
			nil,
		},
		{
			"",
			"",
			errors.New(" is not a valid IP"),
		},
		{
			"foobar",
			"",
			errors.New("foobar is not a valid IP"),
		},
		{
			"127.0.1",
			"",
			errors.New("127.0.1 is not a valid IP"),
		},
		{
			":::",
			"",
			errors.New("::: is not a valid IP"),
		},
	}
	for _, test := range testTable {
		resp, err := apiURL(newMockPod(test.podIP))
		if resp != test.expectedResponse {
			t.Errorf("expected response %v does not match the actual %v", test.expectedResponse, resp)
		}
		if err != test.expectedError {
			if err == nil || test.expectedError == nil {
				t.Errorf("expected error '%v' does not match the actual error '%v'", test.expectedError, err)
			}
			if err != nil && test.expectedError != nil && err.Error() != test.expectedError.Error() {
				t.Errorf("expected error '%v' does not match the actual error '%v'", test.expectedError, err)
			}
		}
	}
}

func TestPatroniAPI(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	json := `{"state": "running", "postmaster_start_time": "2021-02-19 14:31:50.053 CET", "role": "master", "server_version": 90621, "cluster_unlocked": false, "xlog": {"location": 55978296057856}, "timeline": 6, "database_system_identifier": "6462555844314089962", "pending_restart": true, "patroni": {"version": "2.0.1", "scope": "acid-rest92-standby"}}`
	r := ioutil.NopCloser(bytes.NewReader([]byte(json)))

	response := http.Response{
		Status: "200",
		Body:   r,
	}

	mockClient := mocks.NewMockHTTPClient(ctrl)
	mockClient.EXPECT().Get(gomock.Any()).Return(&response, nil)

	p := New(nil, mockClient)

	pod := v1.Pod{
		Status: v1.PodStatus{
			PodIP: "192.168.100.1",
		},
	}
	_, err := p.GetMemberData(&pod)

	if err != nil {
		t.Errorf("Could not read Patroni data: %v", err)
	}
}
