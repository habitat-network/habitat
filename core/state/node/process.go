package node

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type ProcessID string

func NewProcessID(driver DriverType) ProcessID {
	return ProcessID(fmt.Sprintf("%s:%s", driver.String(), uuid.NewString()))
}

func DriverFromProcessID(id ProcessID) (DriverType, error) {
	sep := strings.Split(string(id), ":")
	if len(sep) < 2 {
		return DriverTypeUnknown, fmt.Errorf("malformed processID: %s", id)
	}
	return driverTypeFromString(sep[0]), nil
}

type DriverType string

const (
	DriverTypeUnknown DriverType = "unknown"
	DriverTypeNoop    DriverType = "noop"
	DriverTypeDocker  DriverType = "docker"
	DriverTypeWeb     DriverType = "web"
)

func (d DriverType) String() string {
	return string(d)
}

func driverTypeFromString(s string) DriverType {
	switch s {
	case "docker":
		return DriverTypeDocker
	case "web":
		return DriverTypeWeb
	case "noop":
		return DriverTypeNoop
	}
	return DriverTypeUnknown
}

// Types related to running processes, mostly used by internal/process
type Process struct {
	ID      ProcessID `json:"id"`
	AppID   string    `json:"app_id"`
	UserID  string    `json:"user_id"`
	Created string    `json:"created"`
}
