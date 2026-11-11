package cluster

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/v2/pkg/apis/acid.zalan.do/v1"
)

// fakeSchemaDB is a minimal database/sql driver. Statements that mention
// failOn are rejected, getSchemasSQL returns the schemas created so far.
type fakeSchemaDB struct {
	mu      sync.Mutex
	failOn  string
	schemas []string
	execs   []string
}

type fakeConn struct{ db *fakeSchemaDB }
type fakeRows struct {
	schemas []string
	pos     int
}

func (f *fakeSchemaDB) Open(string) (driver.Conn, error) { return &fakeConn{f}, nil }
func (c *fakeConn) Prepare(q string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return nil, fmt.Errorf("tx not supported") }

func (c *fakeConn) Exec(q string, _ []driver.Value) (driver.Result, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	c.db.execs = append(c.db.execs, q)
	if c.db.failOn != "" && strings.Contains(q, c.db.failOn) {
		return nil, fmt.Errorf("role does not exist")
	}
	if strings.Contains(q, "CREATE SCHEMA") {
		c.db.schemas = append(c.db.schemas, "data")
	}
	return driver.RowsAffected(0), nil
}

func (c *fakeConn) Query(q string, _ []driver.Value) (driver.Rows, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	return &fakeRows{schemas: append([]string{}, c.db.schemas...)}, nil
}

func (r *fakeRows) Columns() []string { return []string{"dbschema"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.schemas) {
		return io.EOF
	}
	dest[0] = r.schemas[r.pos]
	r.pos++
	return nil
}

func (f *fakeSchemaDB) privilegeExecs() (n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, q := range f.execs {
		if strings.Contains(q, "ALTER DEFAULT PRIVILEGES") {
			n++
		}
	}
	return n
}

func TestPreparedSchemaPrivilegeErrors(t *testing.T) {
	driverName := "fake-schema-db-" + strings.ReplaceAll(t.Name(), "/", "-")
	fake := &fakeSchemaDB{failOn: "schema_db_writer"}
	sql.Register(driverName, fake)
	db, err := sql.Open(driverName, "")
	assert.NoError(t, err)
	defer db.Close()

	noDefaults := false
	var cluster Cluster
	cluster.logger = logrus.New().WithField("test", "prepared-schemas")
	cluster.pgDb = db
	cluster.Spec.PreparedDatabases = map[string]acidv1.PreparedDatabase{
		"schema_db": {
			DefaultUsers: false,
			PreparedSchemas: map[string]acidv1.PreparedSchema{
				"data": {DefaultRoles: &noDefaults},
			},
		},
	}
	schemas := cluster.Spec.PreparedDatabases["schema_db"].PreparedSchemas

	// schema is created, but granting privileges fails: error must be returned
	err = cluster.syncPreparedSchemas("schema_db", schemas)
	assert.Error(t, err, "failing default privileges must be reported")
	assert.Contains(t, fmt.Sprint(err), "could not alter default privileges")

	// the schema exists now; a later sync must still report the problem ...
	err = cluster.syncPreparedSchemas("schema_db", schemas)
	assert.Error(t, err, "missing privileges must be retried while the schema exists")

	// ... and succeed once the missing role was created
	fake.mu.Lock()
	fake.failOn = ""
	fake.mu.Unlock()
	before := fake.privilegeExecs()
	err = cluster.syncPreparedSchemas("schema_db", schemas)
	assert.NoError(t, err)
	assert.Greater(t, fake.privilegeExecs(), before, "privileges must be applied on retry")
}
