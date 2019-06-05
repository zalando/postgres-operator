package v1

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	weekdays         = map[string]int{"Sun": 0, "Mon": 1, "Tue": 2, "Wed": 3, "Thu": 4, "Fri": 5, "Sat": 6}
	serviceNameRegex = regexp.MustCompile(serviceNameRegexString)
)

// Clone convenience wrapper around DeepCopy
func (p *Postgresql) Clone() *Postgresql {
	if p == nil {
		return nil
	}
	return p.DeepCopy()
}

func parseTime(s string) (metav1.Time, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return metav1.Time{}, fmt.Errorf("incorrect time format")
	}
	timeLayout := "15:04"

	tp, err := time.Parse(timeLayout, s)
	if err != nil {
		return metav1.Time{}, err
	}

	return metav1.Time{Time: tp.UTC()}, nil
}

func parseWeekday(s string) (time.Weekday, error) {
	weekday, ok := weekdays[s]
	if !ok {
		return time.Weekday(0), fmt.Errorf("incorrect weekday")
	}

	return time.Weekday(weekday), nil
}

func extractClusterName(clusterName string, teamName string) (string, error) {
	teamNameLen := len(teamName)
	if len(clusterName) < teamNameLen+2 {
		return "", fmt.Errorf("cluster name must match {TEAM}-{NAME} format. Got cluster name '%v', team name '%v'", clusterName, teamName)
	}

	if teamNameLen == 0 {
		return "", fmt.Errorf("team name is empty")
	}

	if strings.ToLower(clusterName[:teamNameLen+1]) != strings.ToLower(teamName)+"-" {
		return "", fmt.Errorf("name must match {TEAM}-{NAME} format")
	}
	if len(clusterName) > clusterNameMaxLength {
		return "", fmt.Errorf("name cannot be longer than %d characters", clusterNameMaxLength)
	}
	if !serviceNameRegex.MatchString(clusterName) {
		return "", fmt.Errorf("name must confirm to DNS-1035, regex used for validation is %q",
			serviceNameRegexString)
	}

	return clusterName[teamNameLen+1:], nil
}

func validateCloneClusterDescription(clone *CloneDescription) error {
	// when cloning from the basebackup (no end timestamp) check that the cluster name is a valid service name
	if clone.ClusterName != "" && clone.EndTimestamp == "" {
		if !serviceNameRegex.MatchString(clone.ClusterName) {
			return fmt.Errorf("clone cluster name must confirm to DNS-1035, regex used for validation is %q",
				serviceNameRegexString)
		}
		if len(clone.ClusterName) > serviceNameMaxLength {
			return fmt.Errorf("clone cluster name must be no longer than %d characters", serviceNameMaxLength)
		}
	}
	return nil
}

// Success of the current Status
func (postgresStatus PostgresStatus) Success() bool {
	return postgresStatus.PostgresClusterStatus != ClusterStatusAddFailed &&
		postgresStatus.PostgresClusterStatus != ClusterStatusUpdateFailed &&
		postgresStatus.PostgresClusterStatus != ClusterStatusSyncFailed
}

// Running status of cluster
func (postgresStatus PostgresStatus) Running() bool {
	return postgresStatus.PostgresClusterStatus == ClusterStatusRunning
}

// Creating status of cluster
func (postgresStatus PostgresStatus) Creating() bool {
	return postgresStatus.PostgresClusterStatus == ClusterStatusCreating
}

func (postgresStatus PostgresStatus) String() string {
	return postgresStatus.PostgresClusterStatus
}
