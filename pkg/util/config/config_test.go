package config

import (
	"fmt"
	"reflect"
	"testing"
)

var getMapPairsFromStringTest = []struct {
	in       string
	expected []string
	err      error
}{
	{"log_statement:all, work_mem:'4GB'", []string{"log_statement:all", "work_mem:'4GB'"}, nil},
	{`log_statement:none, search_path:'"$user", public'`, []string{"log_statement:none", `search_path:'"$user", public'`}, nil},
	{`search_path:'"$user"`, nil, fmt.Errorf("unmatched quote starting at position 13")},
	{"", []string{""}, nil},
	{",,log_statement:all	,", []string{"", "", "log_statement:all", ""}, nil},
}

func TestGetMapPairsFromString(t *testing.T) {
	for _, tt := range getMapPairsFromStringTest {
		got, err := getMapPairsFromString(tt.in)
		if err != tt.err && ((err == nil || tt.err == nil) || (err.Error() != tt.err.Error())) {
			t.Errorf("TestGetMapPairsFromString with %s: expected error: %#v, got %#v", tt.in, tt.err, err)
		}
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("TestGetMapPairsFromString with %s: expected %#v, got %#v", tt.in, tt.expected, got)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		args    *Config
		wantErr bool
	}{
		{
			name: "config1",
			args: &Config{
				Resources: Resources{
					MaxInstances: 2,
					MinInstances: 1,
				},
				Workers: 3,
			},
			wantErr: false,
		},
		{
			name: "config2",
			args: &Config{
				Workers: 0,
			},
			wantErr: true,
		},
		{
			name: "config3",
			args: &Config{
				Resources: Resources{
					MaxInstances: 1,
					MinInstances: 2,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}

}
