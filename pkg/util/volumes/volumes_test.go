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
			input:          "aws://eu-central-1c/vol-01234a5b6c78df9gh",
			expectedResult: "vol-01234a5b6c78df9gh",
			expectedErr:    nil,
		},
		{
			input:          "vol-0g9fd87c6b5a43210",
			expectedResult: "vol-0g9fd87c6b5a43210",
			expectedErr:    nil,
		},
		{
			input:          "aws://eu-central-1c/01234a5b6c78df9g0",
			expectedResult: "",
			expectedErr:    fmt.Errorf("malformed EBS volume id %q", "aws://eu-central-1c/01234a5b6c78df9g0"),
		},
		{
			input:          "hg9fd87c6b5a43210",
			expectedResult: "",
			expectedErr:    fmt.Errorf("malformed EBS volume id %q", "hg9fd87c6b5a43210"),
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
