package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/controller"
	node_state "github.com/eagraf/habitat-new/internal/node/state"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var port string

type msg struct {
	Status string `json:"status"`
	Body   any    `json:"body"`
}

func printResponse(res *http.Response) error {
	slurp, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	// Kinda wack, but we want to embed this JSON response into the msg type
	// So unmarshal it and then immediately marshal the whole thing
	var body any
	if res.StatusCode != int(http.StatusOK) {
		body = string(slurp)
	} else if len(slurp) > 0 {
		err = json.Unmarshal(slurp, &body)
		if err != nil {
			return errors.Wrap(err, "error unmarshalling response body")
		}
	}
	resp, err := json.Marshal(msg{
		Status: res.Status,
		Body:   body,
	})
	if err != nil {
		return errors.Wrap(err, "error marshalling response body into msg type")
	}
	fmt.Println(string(resp))
	return nil
}

func startProcess() *cli.Command {
	var req controller.StartProcessRequest
	return &cli.Command{
		Name:  "start",
		Usage: "Start a new process for a given app installation.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "app",
				Usage:       "The name of the desired app for which to start the process.",
				Destination: &req.AppInstallationID,
				Required:    true,
			},
		},
		Action: func(ctx *cli.Context) error {
			url := fmt.Sprintf("http://localhost:%s/node/processes/start", port)
			marshalled, err := json.Marshal(req)
			if err != nil {
				return err
			}
			res, err := http.Post(url, "application/json", bytes.NewReader(marshalled))
			if err != nil {
				return err
			}
			return printResponse(res)
		},
	}
}

func stopProcess() *cli.Command {
	var id string
	var req controller.StopProcessRequest
	return &cli.Command{
		Name:  "stop",
		Usage: "Stop the process with the given ID.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "id",
				Usage:       "The process ID for the desired process to stop.",
				Destination: &id,
				Required:    true,
			},
		},
		Action: func(ctx *cli.Context) error {
			url := fmt.Sprintf("http://localhost:%s/node/processes/stop", port)
			req.ProcessID = node_state.ProcessID(id)
			marshalled, err := json.Marshal(req)
			if err != nil {
				return err
			}
			res, err := http.Post(url, "application/json", bytes.NewReader(marshalled))
			if err != nil {
				return err
			}
			return printResponse(res)
		},
	}
}

func listProcesses() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all running processes",
		Action: func(ctx *cli.Context) error {
			url := fmt.Sprintf("http://localhost:%s/node/processes/list", port)
			res, err := http.Get(url)
			if err != nil {
				return err
			}
			return printResponse(res)
		},
	}
}

func getState() *cli.Command {
	return &cli.Command{
		Name:  "state",
		Usage: "Get node state",
		Action: func(ctx *cli.Context) error {
			url := fmt.Sprintf("http://localhost:%s/node/state", port)
			res, err := http.Get(url)
			if err != nil {
				return err
			}
			return printResponse(res)
		},
	}
}

func listApps() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "Get list of installed apps.",
		Action: func(ctx *cli.Context) error {
			url := fmt.Sprintf("http://localhost:%s/node/apps/list", port)
			res, err := http.Get(url)
			if err != nil {
				return err
			}
			return printResponse(res)
		},
	}
}

func installApp() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "Install an application.",
		Action: func(ctx *cli.Context) error {
			return fmt.Errorf("unimplemented")
		},
	}
}

func uninstallApp() *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "Uninstall an application",
		Action: func(ctx *cli.Context) error {
			return fmt.Errorf("unimplemented")
		},
	}
}

func main() {
	app := &cli.App{
		Name:  "node_ctl",
		Usage: "CLI interface for interacting with the Node Control server",
		CommandNotFound: func(ctx *cli.Context, s string) {
			fmt.Println("command not found: ", s)
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "port",
				Aliases:     []string{"p"},
				Usage:       "Ctrl Server port to connect to",
				Value:       constants.DefaultPortHabitatAPI,
				Destination: &port,
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "process",
				Usage: "Commands related to processes managed by the node.",
				Subcommands: []*cli.Command{
					startProcess(),
					stopProcess(),
					listProcesses(),
				},
			},
			{
				Name:  "node",
				Usage: "Commands related to general node actions.",
				Subcommands: []*cli.Command{
					getState(),
				},
			},
			{
				Name:  "app",
				Usage: "Commands related to app installations managed by the node.",
				Subcommands: []*cli.Command{
					listApps(),
					installApp(),
					uninstallApp(),
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
}
