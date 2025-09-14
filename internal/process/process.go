package process

import (
	"fmt"
	"strings"

	"github.com/eagraf/habitat-new/internal/app"
	"github.com/google/uuid"
)

// TODO: move these types to internal/process
type ID string

func NewID(driver app.DriverType) ID {
	return ID(fmt.Sprintf("%s:%s", driver.String(), uuid.NewString()))
}

func driverFromID(id ID) (app.DriverType, error) {
	sep := strings.Split(string(id), ":")
	if len(sep) < 2 {
		return app.DriverTypeUnknown, fmt.Errorf("malformed processID: %s", id)
	}
	return app.DriverTypeFromString(sep[0]), nil
}

// Types related to running processes, mostly used by internal/process
type Process struct {
	ID      ID     `json:"id"`
	AppID   string `json:"app_id"`
	UserID  string `json:"user_id"`
	Created string `json:"created"`
}
