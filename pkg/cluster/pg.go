package cluster

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

var createUserSQL = `SET LOCAL synchronous_commit = 'local'; CREATE ROLE "%s" %s PASSWORD %s;`

func (c *Cluster) pgConnectionString() string {
	hostname := fmt.Sprintf("%s.%s.svc.cluster.local", c.Metadata.Name, c.Metadata.Namespace)
	password := c.pgUsers[constants.SuperuserName].Password

	return fmt.Sprintf("host='%s' dbname=postgres sslmode=require user='%s' password='%s'",
		hostname,
		constants.SuperuserName,
		strings.Replace(password, "$", "\\$", -1))
}

func (c *Cluster) initDbConn() error {
	//TODO: concurrent safe?
	if c.pgDb == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.pgDb == nil {
			conn, err := sql.Open("postgres", c.pgConnectionString())
			if err != nil {
				return err
			}
			err = conn.Ping()
			if err != nil {
				return err
			}

			c.pgDb = conn
		}
	}

	return nil
}

func (c *Cluster) createPgUser(user spec.PgUser) (isHuman bool, err error) {
	var flags []string = user.Flags

	if user.Password == "" {
		isHuman = true
		flags = append(flags, fmt.Sprintf("IN ROLE \"%s\"", constants.PamRoleName))
	} else {
		isHuman = false
	}

	addLoginFlag := true
	for _, v := range flags {
		if v == "NOLOGIN" {
			addLoginFlag = false
			break
		}
	}
	if addLoginFlag {
		flags = append(flags, "LOGIN")
	}

	userFlags := strings.Join(flags, " ")
	userPassword := fmt.Sprintf("'%s'", user.Password)
	if user.Password == "" {
		userPassword = "NULL"
	}
	query := fmt.Sprintf(createUserSQL, user.Name, userFlags, userPassword)

	_, err = c.pgDb.Query(query) // TODO: Try several times
	if err != nil {
		err = fmt.Errorf("DB error: %s", err)
		return
	}

	return
}
