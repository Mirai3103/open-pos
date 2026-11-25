//go:build integration

package inventory_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/open-pos/internal/testdb"
)

var invTestDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "inventory", func(db *sql.DB) { invTestDB = db }))
}
