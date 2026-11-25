//go:build integration

package features_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/open-pos/internal/testdb"
)

var featuresTestDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "features", func(db *sql.DB) { featuresTestDB = db }))
}
