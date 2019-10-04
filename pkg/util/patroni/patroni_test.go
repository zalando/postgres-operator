package patroni

import (
	"errors"
	"fmt"
	"k8s.io/api/core/v1"
	"testing"
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
