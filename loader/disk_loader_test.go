package loader

import (
	"context"
	"testing"

	"github.com/lukaszbudnik/migrator/config"
	"github.com/stretchr/testify/assert"
)

func TestDiskReadDiskMigrationsNonExistingBaseDirError(t *testing.T) {
	var config config.Config
	config.BaseDir = "xyzabc"
	config.SingleMigrations = []string{"migrations/config"}

	loader := New(context.TODO(), &config)

	didPanic := false
	var message interface{}
	func() {

		defer func() {
			if message = recover(); message != nil {
				didPanic = true
			}
		}()

		loader.GetSourceMigrations()

	}()
	assert.True(t, didPanic)
	assert.Contains(t, message, "xyzabc/migrations/config: no such file or directory")
}

func TestDiskReadDiskMigrationsNonExistingMigrationsDirError(t *testing.T) {
	var config config.Config
	config.BaseDir = "../test"
	config.SingleMigrations = []string{"migrations/abcdef"}

	loader := New(context.TODO(), &config)

	didPanic := false
	var message interface{}
	func() {

		defer func() {
			if message = recover(); message != nil {
				didPanic = true
			}
		}()

		loader.GetSourceMigrations()

	}()
	assert.True(t, didPanic)
	assert.Contains(t, message, "test/migrations/abcdef: no such file or directory")
}

func TestDiskGetDiskMigrations(t *testing.T) {
	var config config.Config
	config.BaseDir = "../test"
	config.SingleMigrations = []string{"migrations/config", "migrations/ref"}
	config.TenantMigrations = []string{"migrations/tenants"}
	config.SingleScripts = []string{"migrations/config-scripts"}
	config.TenantScripts = []string{"migrations/tenants-scripts"}

	loader := New(context.TODO(), &config)
	migrations := loader.GetSourceMigrations()

	assert.Len(t, migrations, 10)

	assert.Contains(t, migrations[0].File, "test/migrations/config/201602160001.sql")
	assert.Contains(t, migrations[1].File, "test/migrations/config/201602160002.sql")
	assert.Contains(t, migrations[2].File, "test/migrations/tenants/201602160002.sql")
	assert.Contains(t, migrations[3].File, "test/migrations/ref/201602160003.sql")
	assert.Contains(t, migrations[4].File, "test/migrations/tenants/201602160003.sql")
	assert.Contains(t, migrations[5].File, "test/migrations/ref/201602160004.sql")
	assert.Contains(t, migrations[6].File, "test/migrations/tenants/201602160004.sql")
	assert.Contains(t, migrations[7].File, "test/migrations/tenants/201602160005.sql")
	assert.Contains(t, migrations[8].File, "test/migrations/config-scripts/201912181227.sql")
	assert.Contains(t, migrations[9].File, "test/migrations/tenants-scripts/201912181228.sql")
}
