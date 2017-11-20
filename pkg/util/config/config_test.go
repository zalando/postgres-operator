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
