package cluster

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
)

var getUserSQL = `SELECT a.rolname, COALESCE(a.rolpassword, ''), a.rolsuper, a.rolinherit,
	        a.rolcreaterole, a.rolcreatedb, a.rolcanlogin,
	        ARRAY(SELECT b.rolname
	              FROM pg_catalog.pg_auth_members m
	              JOIN pg_catalog.pg_authid b ON (m.roleid = b.oid)
	             WHERE m.member = a.oid) as memberof
	 FROM pg_catalog.pg_authid a
	 WHERE a.rolname = ANY($1)
	 ORDER BY 1;`

func (c *Cluster) pgConnectionString() string {
	hostname := fmt.Sprintf("%s.%s.svc.cluster.local", c.Name, c.Namespace)
	username := c.systemUsers[constants.SuperuserKeyName].Name
	password := c.systemUsers[constants.SuperuserKeyName].Password

	return fmt.Sprintf("host='%s' dbname=postgres sslmode=require user='%s' password='%s'",
		hostname,
		username,
		strings.Replace(password, "$", "\\$", -1))
}

func (c *Cluster) databaseAccessDisabled() bool {
	if !c.OpConfig.EnableDBAccess {
		c.logger.Debugf("database access is disabled")
	}

	return !c.OpConfig.EnableDBAccess
}

func (c *Cluster) initDbConn() (err error) {
	if c.pgDb == nil {
		conn, err := sql.Open("postgres", c.pgConnectionString())
		if err != nil {
			return err
		}
		c.logger.Debug("new database connection")
		err = conn.Ping()
		if err != nil {
			if err2 := conn.Close(); err2 != nil {
				c.logger.Errorf("error when closing PostgreSQL connection after another error: %v", err2)
			}
			return err
		}

		c.pgDb = conn
	}

	return nil
}

func (c *Cluster) closeDbConn() (err error) {
	if c.pgDb != nil {
		c.logger.Debug("closing database connection")
		if err = c.pgDb.Close(); err != nil {
			c.logger.Errorf("could not close database connection: %v", err)
		}
		c.pgDb = nil

		return nil
	}
	c.logger.Warning("attempted to close an empty db connection object")
	return nil
}

func (c *Cluster) readPgUsersFromDatabase(userNames []string) (users spec.PgUserMap, err error) {
	var rows *sql.Rows
	users = make(spec.PgUserMap)
	if rows, err = c.pgDb.Query(getUserSQL, pq.Array(userNames)); err != nil {
		return nil, fmt.Errorf("error when querying users: %v", err)
	}
	defer func() {
		if err2 := rows.Close(); err2 != nil {
			err = fmt.Errorf("error when closing query cursor: %v", err2)
		}
	}()

	for rows.Next() {
		var (
			rolname, rolpassword                                          string
			rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin bool
			memberof                                                      []string
		)
		err := rows.Scan(&rolname, &rolpassword, &rolsuper, &rolinherit,
			&rolcreaterole, &rolcreatedb, &rolcanlogin, pq.Array(&memberof))
		if err != nil {
			return nil, fmt.Errorf("error when processing user rows: %v", err)
		}
		flags := makeUserFlags(rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin)
		// XXX: the code assumes the password we get from pg_authid is always MD5
		users[rolname] = spec.PgUser{Name: rolname, Password: rolpassword, Flags: flags, MemberOf: memberof}
	}

	return users, nil
}

func makeUserFlags(rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin bool) (result []string) {
	if rolsuper {
		result = append(result, constants.RoleFlagSuperuser)
	}
	if rolinherit {
		result = append(result, constants.RoleFlagInherit)
	}
	if rolcreaterole {
		result = append(result, constants.RoleFlagCreateRole)
	}
	if rolcreatedb {
		result = append(result, constants.RoleFlagCreateDB)
	}
	if rolcanlogin {
		result = append(result, constants.RoleFlagLogin)
	}

	return result
}
