package cluster

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// this is a mock struct that represents
// the Connector interface from the database/sql/driver package
type mockConnector struct {
	rows driver.Rows
}

func (m *mockConnector) Connect(context.Context) (driver.Conn, error) {
	return &mockConn{rows: m.rows}, nil
}

func (m *mockConnector) Driver() driver.Driver {
	return nil
}

// this is a mock struct that represents
// the Connection interface from the database/sql/driver package
type mockConn struct {
	rows driver.Rows
}

func (m *mockConn) Prepare(query string) (driver.Stmt, error) {
	return nil, nil
}

func (m *mockConn) Begin() (driver.Tx, error) {
	return nil, nil
}

func (m *mockConn) Close() error {
	return nil
}

// the reason we need this is because sql.DB.Query calls QueryerContext
// here the fakeConn is implicitly implementing the QueryerContext interface
func (m *mockConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return m.rows, nil
}

// this is the mock struct that represents
// the Rows interface from the database/sql/driver package
type fakeRows struct {
	rows    [][]driver.Value
	rowErrs map[int]error
	pos     int
}

func (f *fakeRows) Columns() []string {
	return []string{"datname", "owner"}
}

func (f *fakeRows) Close() error {
	return nil
}

func (f *fakeRows) Next(dest []driver.Value) error {
	if f.pos >= len(f.rows) {
		return io.EOF
	}

	if err := f.rowErrs[f.pos]; err != nil {
		f.pos++
		return err
	}

	copy(dest, f.rows[f.pos])
	f.pos++
	return nil
}

func TestGetDatabases(t *testing.T) {
	tests := []struct {
		name    string
		rows    *fakeRows
		want    map[string]string
		wantErr string
	}{
		{
			name: "success",
			rows: &fakeRows{
				rows: [][]driver.Value{
					{"postgres", "postgres"},
					{"test_db", "test_owner"},
				},
			},
			want: map[string]string{
				"postgres": "postgres",
				"test_db":  "test_owner",
			},
		},
		{
			name: "partial result returns error",
			rows: &fakeRows{
				rows: [][]driver.Value{
					{"postgres", "postgres"},
					{"test_db", "test_owner"},
				},
				rowErrs: map[int]error{1: errors.New("connection reset")},
			},
			wantErr: "error when iterating database rows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := sql.OpenDB(&mockConnector{rows: tt.rows})
			defer db.Close()

			c := &Cluster{pgDb: db}
			got, err := c.getDatabases()

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
