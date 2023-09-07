package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadValidateDatabaseNameRegexp(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		wantErrMsg string
	}{
		{"default value", databaseNameRegexp.String(), ""},
		{"null value", "", "validation is not set"},
		{"wrong expression", "12(@3202@@!)#)$$%#$_!@@!_*%_@", "validation is not correct"},
		{"correct expression", "^[a-z0-9]([-_a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-_a-z0-9]*[a-z0-9])?)*$", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cl.readValidateDatabaseNameRegexp(tt.expression)
			if len(tt.wantErrMsg) > 0 {
				assert.Containsf(t, err.Error(), tt.wantErrMsg, "expected error containing %q, got %s", tt.wantErrMsg, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
