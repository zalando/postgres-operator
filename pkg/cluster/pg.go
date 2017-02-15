package cluster

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
	"strings"
)

func (c *Cluster) pgConnectionString() string {
	hostname := fmt.Sprintf("%s.%s.svc.cluster.local", (*c.cluster).Metadata.Name, (*c.cluster).Metadata.Namespace)
	password := c.pgUsers[superUsername].password

	return fmt.Sprintf("host='%s' dbname=postgres sslmode=require user=postgres password='%s'",
		hostname,
		strings.Replace(password, "$", "\\$", -1))
}

func (c *Cluster) initDbConn() error {
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
