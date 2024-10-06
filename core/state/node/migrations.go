package node

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/wI2L/jsondiff"
	"golang.org/x/mod/semver"
)

// TODO factor this out to be reusable across schemas

//go:embed migrations/*
var migrationsDir embed.FS

type JSONPatch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

type Migration struct {
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`

	Up   []JSONPatch `json:"up"`
	Down []JSONPatch `json:"down"`

	upBytes   []byte
	downBytes []byte
}

func getSchemaVersion(migrations []*Migration, targetVersion string) ([]byte, error) {
	// TODO validate up to a certain point. Right now this just validates
	// all migrations up to the full schema, but we might want to stop at an
	// intermediate state.

	curSchema := "{}"
	for _, mig := range migrations {

		patch, err := jsonpatch.DecodePatch(mig.upBytes)
		if err != nil {
			return nil, err
		}

		updated, err := patch.Apply([]byte(curSchema))
		if err != nil {
			return nil, err
		}

		curSchema = string(updated)

		if mig.NewVersion == targetVersion {
			return updated, nil
		} else if semver.Compare(mig.NewVersion, targetVersion) > 0 {
			return nil, fmt.Errorf("target version %s not found in migrations. latest version found: %s", targetVersion, mig.NewVersion)
		}
	}

	return nil, fmt.Errorf("target version not found in migrations: %s", targetVersion)
}

func readSchemaMigrationFiles() ([]*Migration, error) {
	migrationsFiles, err := migrationsDir.ReadDir("migrations")
	if err != nil {
		return nil, err
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
			return nil, err
		}

		var migration Migration
		err = json.Unmarshal(migFileData, &migration)
		if err != nil {
			return nil, err
		}

		// Convert the JSONPatch arrays to bytes
		migration.upBytes, err = json.Marshal(migration.Up)
		if err != nil {
			return nil, err
		}
		migration.downBytes, err = json.Marshal(migration.Down)
		if err != nil {
			return nil, err
		}

		migrations = append(migrations, &migration)
	}

	return migrations, nil
}

func compareSchemas(expected, actual string) error {
	var expectedMap, actualMap map[string]interface{}
	err := json.Unmarshal([]byte(expected), &expectedMap)
	if err != nil {
		return err
	}

	if err := json.Unmarshal([]byte(actual), &actualMap); err != nil {
		return err
	}

	patch, err := jsondiff.Compare(expectedMap, actualMap)
	if err != nil {
		return err
	}

	if len(patch) > 0 {
		// Pretty print the JSON in the error so it's easy to read.
		indented := &bytes.Buffer{}
		encoder := json.NewEncoder(indented)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(patch); err != nil {
			return fmt.Errorf("failed to indent JSON patch: %v", err)
		}
		// Pretty print the actual and expected JSON for easier comparison
		indentedActual := &bytes.Buffer{}
		indentedExpected := &bytes.Buffer{}
		err = json.Indent(indentedActual, []byte(actual), "", "  ")
		if err != nil {
			return err
		}
		err = json.Indent(indentedExpected, []byte(expected), "", "  ")
		if err != nil {
			return err
		}

		return fmt.Errorf("schemas are not equal.\n\nDiff patch:\n%s\n\nExpected:\n%s\n\nActual:\n%s",
			indented.String(),
			indentedExpected.String(),
			indentedActual.String())
	}

	return nil
}

type MigrationsList []DataMigration

// GetNeededMigrations returns the list of migrations needed to go from the current version to the target version
// This works in both forward and reverse orders. The migrations are returned in the order they should run.
// Assumes the migration list is sorted oldest to newest.
func (m MigrationsList) GetNeededMigrations(currentVersion, targetVersion string) ([]DataMigration, error) {
	if semver.Compare(currentVersion, targetVersion) == 0 {
		return nil, fmt.Errorf("current version and target version are the same")
	}

	migrations := make([]DataMigration, 0)
	inMigration := false
	for _, dataMigration := range NodeDataMigrations {
		if dataMigration.DownVersion() == currentVersion && !inMigration {
			inMigration = true
			migrations = append(migrations, dataMigration)
		} else if (dataMigration.UpVersion() == currentVersion || dataMigration.UpVersion() == targetVersion) && !inMigration {
			inMigration = true
			continue
		} else if inMigration {
			migrations = append(migrations, dataMigration)
		}
		if (dataMigration.UpVersion() == targetVersion || dataMigration.UpVersion() == currentVersion) && inMigration {
			inMigration = false
			break
		}
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("no migrations found")
	}
	if inMigration {
		return nil, fmt.Errorf("couldn't find full migration range, latest was %s", migrations[len(migrations)-1].UpVersion())
	}

	// If going downward, reverse the order of the migrations
	if semver.Compare(currentVersion, targetVersion) > 0 {
		downMigrations := make([]DataMigration, len(migrations))
		for i := len(migrations) - 1; i >= 0; i-- {
			downMigrations[len(migrations)-1-i] = migrations[i]
		}
		return downMigrations, nil
	}

	return migrations, nil
}

func (m MigrationsList) GetMigrationPatch(currentVersion, targetVersion string, startState *State) (jsondiff.Patch, error) {
	migrations, err := m.GetNeededMigrations(currentVersion, targetVersion)
	if err != nil {
		return nil, err
	}
	curState, err := startState.Copy()
	if err != nil {
		return nil, err
	}
	for _, dataMigration := range migrations {
		// Run the up migration
		if semver.Compare(currentVersion, targetVersion) < 0 {
			curState, err = dataMigration.Up(curState)
			if err != nil {
				return nil, err
			}
		} else if semver.Compare(currentVersion, targetVersion) > 0 {
			curState, err = dataMigration.Down(curState)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("current version and target version are the same, but at least one migration was still selected. this should never happen. current: %s, target: %s", currentVersion, targetVersion)
		}
		if dataMigration.UpVersion() == targetVersion {
			break
		}
	}

	curState.SchemaVersion = targetVersion

	patch, err := jsondiff.Compare(startState, curState)
	if err != nil {
		return nil, err
	}

	return patch, nil
}

type DataMigration interface {
	UpVersion() string
	DownVersion() string
	Up(*State) (*State, error)
	Down(*State) (*State, error)
}

// basicDataMigration is a simple implementation of DataMigration with some basic helpers.
type basicDataMigration struct {
	upVersion   string
	downVersion string

	// Functions for moving data up and down a version
	up   func(*State) (*State, error)
	down func(*State) (*State, error)
}

func (m *basicDataMigration) UpVersion() string {
	return m.upVersion
}

func (m *basicDataMigration) DownVersion() string {
	return m.downVersion
}

func (m *basicDataMigration) Up(state *State) (*State, error) {
	return m.up(state)
}

func (m *basicDataMigration) Down(state *State) (*State, error) {
	return m.down(state)
}

// Rules for migrations:
// - Going upwards, fields can only be added not removed. New fields must be optional
// - Going downwards, fields can only be removed not added. Removed fields must be optional
var NodeDataMigrations = MigrationsList{
	&basicDataMigration{
		upVersion:   "v0.0.1",
		downVersion: "v0.0.0",
		up: func(state *State) (*State, error) {
			return &State{
				SchemaVersion:    "v0.0.1",
				Users:            make(map[string]*User),
				Processes:        make(map[string]*ProcessState),
				AppInstallations: make(map[string]*AppInstallationState),
			}, nil
		},
		down: func(state *State) (*State, error) {
			// The first down migration can never be run
			return nil, nil
		},
	},
	&basicDataMigration{
		upVersion:   "v0.0.2",
		downVersion: "v0.0.1",
		up: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			newState.TestField = "test"
			return newState, nil
		},
		down: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			newState.TestField = ""
			return newState, nil
		},
	},
	&basicDataMigration{
		upVersion:   "v0.0.3",
		downVersion: "v0.0.2",
		up: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			newState.TestField = ""
			return newState, nil
		},
		down: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			newState.TestField = "test"
			return newState, nil
		},
	},
	&basicDataMigration{
		upVersion:   "v0.0.4",
		downVersion: "v0.0.3",
		up: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			for _, appInstallation := range newState.AppInstallations {
				appInstallation.DriverConfig = map[string]interface{}{}
			}
			rules := make(map[string]*ReverseProxyRule)
			newState.ReverseProxyRules = &rules
			return newState, nil
		},
		down: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			for _, appInstallation := range newState.AppInstallations {
				appInstallation.DriverConfig = nil
			}
			newState.ReverseProxyRules = nil
			return newState, nil
		},
	},
	&basicDataMigration{
		upVersion:   "v0.0.5",
		downVersion: "v0.0.4",
		up: func(state *State) (*State, error) {
			return state, nil
		},
		down: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			for _, user := range newState.Users {
				user.AtprotoDID = ""
			}
			return newState, nil
		},
	},
	&basicDataMigration{
		upVersion:   "v0.0.6",
		downVersion: "v0.0.5",
		up: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}
			return newState, nil
		},
		down: func(state *State) (*State, error) {
			newState, err := state.Copy()
			if err != nil {
				return nil, err
			}

			return newState, nil
		},
	},
}

func applyPatchToState(diffPatch jsondiff.Patch, state *State) (*State, error) {
	stateBytes, err := state.Bytes()
	if err != nil {
		return nil, err
	}

	patchBytes, err := json.Marshal(diffPatch)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		return nil, err
	}

	updated, err := patch.Apply(stateBytes)
	if err != nil {
		return nil, err
	}

	var updatedNodeState *State
	err = json.Unmarshal(updated, &updatedNodeState)
	if err != nil {
		return nil, err
	}

	return updatedNodeState, nil
}

func GetEmptyStateForVersion(version string) (*State, error) {

	emptyState := &State{}

	diffPatch, err := NodeDataMigrations.GetMigrationPatch("v0.0.0", version, emptyState)
	if err != nil {
		return nil, err
	}

	initState, err := applyPatchToState(diffPatch, emptyState)
	if err != nil {
		return nil, err
	}

	return initState, nil
}
