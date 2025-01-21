package node

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/assert"
)

func TestSchemaMigrations(t *testing.T) {
	// Test applying all migrations one at a time, and make sure the finalresult
	// matches the schema.json file. Then go back down, and make sure we start
	// from the same initial empty state.
	err := validateSchemaMigrations()
	if err != nil {
		t.Error(err)
	}

	// Test migrating directly to the latest version. This path is more similar to
	// what the node will actually execute.
	//
	// NOTE: if this test is failing, check that the LatestVersion reflects the most
	// recent migrations you have written.
	err = validateNodeSchemaMigrations(LatestVersion)
	if err != nil {
		t.Error(err)
	}

	// Test non-existant high version
	err = validateNodeSchemaMigrations("v1000.0.1")
	assert.NotNil(t, err)

	// Test skipped version
	err = validateNodeSchemaMigrations("v0.0.2-rc1")
	assert.NotNil(t, err)
}

func TestDataMigrations(t *testing.T) {

	initState, err := GetEmptyStateForVersion("v0.0.1")
	if err != nil {
		t.Error(err)
	}

	diffPatch, err := NodeDataMigrations.GetMigrationPatch("v0.0.1", "v0.0.2", initState)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(diffPatch))

	updated, err := applyPatchToState(diffPatch, initState)
	assert.Nil(t, err)
	assert.Equal(t, "test", updated.TestField)
	assert.Equal(t, "v0.0.2", updated.SchemaVersion)

	err = updated.Validate()
	assert.Nil(t, err)

	diffPatch2, err := NodeDataMigrations.GetMigrationPatch("v0.0.2", "v0.0.3", updated)
	assert.Nil(t, err)

	updated2, err := applyPatchToState(diffPatch2, updated)
	assert.Nil(t, err)
	assert.Equal(t, "", updated2.TestField)
	assert.Equal(t, "v0.0.3", updated2.SchemaVersion)

	err = updated2.Validate()
	assert.Nil(t, err)

	// Test migrating downwards
	diffPatch3, err := NodeDataMigrations.GetMigrationPatch("v0.0.3", "v0.0.2", updated2)
	assert.Nil(t, err)

	updated3, err := applyPatchToState(diffPatch3, updated2)
	assert.Nil(t, err)
	assert.Equal(t, "test", updated3.TestField)
	assert.Equal(t, "v0.0.2", updated3.SchemaVersion)

	// Test migrating to a nonsensically high version
	_, err = NodeDataMigrations.GetMigrationPatch("v0.0.1", "v1000.0.4", initState)
	assert.NotNil(t, err)
}

func TestValidationFailure(t *testing.T) {
	// Test that validation fails when the state is invalid
	nodeSchema := &NodeSchema{}
	state, err := nodeSchema.EmptyState()
	assert.Nil(t, err)

	state.(*State).TestField = "test"
	err = state.Validate()
	assert.NotNil(t, err)
}

func TestBackwardsCompatibility(t *testing.T) {
	// Migrate the data all the way up, and then back down. This test is to ensure backwards
	// compatibility over long periods. It doesn't insert new data in migrations in the middle,
	// so it is mostly testing that data from very early versions will travel up and down correctly.
	// Specific important migrations should have their own tests.
	//
	// WARNING: Any code that breaks this test breaks backwards compatibility. Think hard before
	// changing this test to make your code pass.

	// NodeSchema in v0.0.1
	nodeState := &State{
		NodeID:        "node1",
		Name:          "My Node",
		Certificate:   "Fake certificate",
		SchemaVersion: "v0.0.1",
		Users: map[string]*User{
			"user1": {
				ID:          "user1",
				Username:    "username1",
				Certificate: "fake user certificate",
			},
		},
		AppInstallations: map[string]*AppInstallationState{
			"app1": {
				AppInstallation: &AppInstallation{
					ID:      "app1",
					Name:    "appname1",
					Version: "1.0.0",
					Package: Package{
						Driver:             "docker",
						RegistryURLBase:    "https://registry.example.com",
						RegistryPackageID:  "appname1",
						RegistryPackageTag: "1.0.0",
					},
				},
				State: AppLifecycleStateInstalled,
			},
			"app2": {
				AppInstallation: &AppInstallation{
					ID:      "app2",
					Name:    "appname2",
					Version: "1.0.0",
					Package: Package{
						Driver:             "docker",
						RegistryURLBase:    "https://registry.example.com",
						RegistryPackageID:  "appname1",
						RegistryPackageTag: "1.0.0",
					},
				},
				State: AppLifecycleStateInstalled,
			},
		},
		Processes: map[string]*Process{
			"proc1": &Process{
				ID:      "proc1",
				AppID:   "app1",
				UserID:  "user1",
				Created: "now",
				Driver:  "docker",
			},
			// This process was not in a running state, but should be started
			"proc2": &Process{
				ID:      "proc2",
				AppID:   "app2",
				UserID:  "user1",
				Created: "now",
				Driver:  "docker",
			},
		},
	}

	diffPatch, err := NodeDataMigrations.GetMigrationPatch("v0.0.1", LatestVersion, nodeState)
	assert.Nil(t, err)

	updated, err := applyPatchToState(diffPatch, nodeState)
	assert.Nil(t, err)

	// Validate against the latest schema
	err = updated.Validate()
	assert.Nil(t, err)

	// Migrate back down
	downPatch, err := NodeDataMigrations.GetMigrationPatch(LatestVersion, "v0.0.1", updated)
	assert.Nil(t, err)

	updatedDown, err := applyPatchToState(downPatch, updated)
	assert.Nil(t, err)

	// Assert the serialized version of the beginning and end state are the same
	// Pretty print them so that the diff is easilly viewable in test output
	serialized, err := json.MarshalIndent(nodeState, "", "  ")
	assert.Nil(t, err)

	serializedDown, err := json.MarshalIndent(updatedDown, "", "  ")
	assert.Nil(t, err)

	assert.Equal(t, string(serialized), string(serializedDown))
}

// Helpers for testing migrating schemas up and down
func validateSchemaMigrations() error {
	// TODO validate up to a certain point. Right now this just validates
	// all migrations up to the full schema, but we might want to stop at an
	// intermediate state.

	// WARNING: Any code that breaks this test breaks backwards compatibility. Think hard before
	// changing this test to make your code pass.

	migrationsFiles, err := migrationsDir.ReadDir("migrations")
	if err != nil {
		return err
	}

	// Read in all the migration data
	migrations := make([]*Migration, 0)
	for _, migFile := range migrationsFiles {
		if migFile.IsDir() {
			continue
		}
		if !strings.HasSuffix(migFile.Name(), ".json") {
			continue
		}

		migFileData, err := migrationsDir.ReadFile("migrations/" + migFile.Name())
		if err != nil {
			return err
		}

		var migration Migration
		err = json.Unmarshal(migFileData, &migration)
		if err != nil {
			return err
		}

		// Convert the JSONPatch arrays to bytes
		migration.upBytes, err = json.Marshal(migration.Up)
		if err != nil {
			return err
		}
		migration.downBytes, err = json.Marshal(migration.Down)
		if err != nil {
			return err
		}

		migrations = append(migrations, &migration)
	}

	curSchema := "{}"

	// Test going up
	for _, mig := range migrations {

		patch, err := jsonpatch.DecodePatch(mig.upBytes)
		if err != nil {
			return err
		}

		updated, err := patch.Apply([]byte(curSchema))
		if err != nil {
			return err
		}

		curSchema = string(updated)
	}

	err = compareSchemas(nodeSchemaRaw, curSchema)
	if err != nil {
		return err
	}

	// Test going down
	for i := len(migrations) - 1; i >= 0; i-- {
		mig := migrations[i]

		patch, err := jsonpatch.DecodePatch(mig.downBytes)
		if err != nil {
			return err
		}

		updated, err := patch.Apply([]byte(curSchema))
		if err != nil {
			return err
		}

		curSchema = string(updated)
	}

	if curSchema != "{}" {
		return fmt.Errorf("down migrations do not result in {}")
	}

	return nil
}

func validateNodeSchemaMigrations(targetVersion string) error {
	migrations, err := readSchemaMigrationFiles()
	if err != nil {
		return err
	}

	schema, err := getSchemaVersion(migrations, targetVersion)
	if err != nil {
		return err
	}

	err = compareSchemas(nodeSchemaRaw, string(schema))
	if err != nil {
		return err
	}

	return nil
}
