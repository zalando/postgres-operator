package cluster

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeDBConnector is a minimal database/sql connector recording executed statements.
type fakeDBConnector struct {
	failures int // number of upcoming statements that fail
	execs    []string
}

func (f *fakeDBConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeDBConn{f}, nil
}

func (f *fakeDBConnector) Driver() driver.Driver { return nil }

type fakeDBConn struct{ f *fakeDBConnector }

func (c *fakeDBConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (c *fakeDBConn) Close() error                        { return nil }
func (c *fakeDBConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

func (c *fakeDBConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.f.execs = append(c.f.execs, query)
	if c.f.failures > 0 {
		c.f.failures--
		return nil, errors.New(`role "global_db_writer" does not exist`)
	}
	return driver.RowsAffected(0), nil
}

func TestSyncGlobalDefaultPrivilegesRetry(t *testing.T) {
	connector := &fakeDBConnector{failures: 1}
	var cluster Cluster
	cluster.pgDb = sql.OpenDB(connector)
	defer cluster.pgDb.Close()

	// first attempt fails: the error is reported, so the sync run is marked as failed
	errs := cluster.syncGlobalDefaultPrivileges("global_db", false)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "could not alter default privileges for database global_db")

	// a later sync runs the grants again for the already existing database
	errs = cluster.syncGlobalDefaultPrivileges("global_db", false)
	assert.Empty(t, errs)
	assert.Len(t, connector.execs, 2)
	for _, q := range connector.execs {
		assert.True(t, strings.Contains(q, "ALTER DEFAULT PRIVILEGES"))
		assert.Contains(t, q, "global_db_owner")
	}

	// with default users the owner_user role gets the grants as well
	connector.execs = nil
	errs = cluster.syncGlobalDefaultPrivileges("global_db", true)
	assert.Empty(t, errs)
	assert.Len(t, connector.execs, 2)
}
