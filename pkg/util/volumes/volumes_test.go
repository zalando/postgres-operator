package volumes

import (
	"fmt"
	"testing"
)

func TestExtractVolumeID(t *testing.T) {
	var tests = []struct {
		input          string
		expectedResult string
		expectedErr    error
	}{
		{
			input:          "aws://eu-central-1c/vol-084b93528a2088348",
			expectedResult: "vol-084b93528a2088348",
			expectedErr:    nil,
		},
		{
			input:          "vol-07944cf16c8d8c579",
			expectedResult: "vol-07944cf16c8d8c579",
			expectedErr:    nil,
		},
		{
			input:          "aws://eu-central-1c/09e4d6662560d622d",
			expectedResult: "",
			expectedErr:    fmt.Errorf("malformed EBS volume id %q", "aws://eu-central-1c/09e4d6662560d622d"),
		},
		{
			input:          "09e4d6662560d622d",
			expectedResult: "",
			expectedErr:    fmt.Errorf("malformed EBS volume id %q", "09e4d6662560d622d"),
		},
	}

	resizer := EBSVolumeResizer{}

	for _, tt := range tests {
		volumeId, err := resizer.ExtractVolumeID(tt.input)
		if volumeId != tt.expectedResult {
			t.Errorf("%s expected: %s, got %s", t.Name(), tt.expectedResult, volumeId)
		}
		if err != tt.expectedErr {
			if tt.expectedErr != nil && err.Error() != tt.expectedErr.Error() {
				t.Errorf("%s unexpected error: got %v", t.Name(), err)
			}
		}
	}
}
