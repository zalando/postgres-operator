package config

import (
	"testing"
)

func TestProcessField(t *testing.T) {
	cfg := Config{}
	fields, _ := structFields(&cfg)
	mapFields := map[string]fieldInfo{}

	for _, field := range fields {
		mapFields[field.Name] = field
	}

	tests := []struct {
		name    string
		args    map[string]string
		wantErr bool
	}{
		{
			name: "AllFields-ok",
			args: map[string]string{
				"etcd_host":                  "host1",
				"enable_database_access":     "false",
				"api_port":                   "1234",
				"custom_service_annotations": `{"key":"value"}`,
				"protected_role_names":       `["test1", "test2"]`,
			},
			wantErr: false,
		},
		{
			name:    "APIPort-error",
			args:    map[string]string{"api_port": "string"},
			wantErr: true,
		},
		{
			name:    "CustomServiceAnnotations-error",
			args:    map[string]string{"custom_service_annotations": `,["key":`},
			wantErr: true,
		},
		{
			name:    "EnableDBAccess-error",
			args:    map[string]string{"enable_database_access": "string"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.args {
				err := ProcessField(v, mapFields[k].Field)
				if (err != nil) != tt.wantErr {
					t.Errorf("ProcessField() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
			}
		})
	}
}
