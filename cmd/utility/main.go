package main

import (
	"log"
	"os"

	"github.com/lcpu-club/lfs-auto-grader/pkg/aoiclient"
	"github.com/urfave/cli/v2"
)

var client *aoiclient.Client

func main() {

	app := cli.NewApp()

	app.Name = "lfs-grader-utility"
	app.Usage = "Utility for LFS Auto Grader"

	app.Flags = append(app.Flags, &cli.StringFlag{
		Name:        "endpoint",
		Aliases:     []string{"e"},
		Usage:       "API endpoint",
		Value:       "https://hpcgame.pku.edu.cn",
		DefaultText: "https://hpcgame.pku.edu.cn",
		EnvVars:     []string{"ENDPOINT"},
	})
	app.Flags = append(app.Flags, &cli.StringFlag{
		Name:    "runner-id",
		Aliases: []string{"id"},
		Usage:   "Runner ID",
		EnvVars: []string{"RUNNER_ID"},
	})
	app.Flags = append(app.Flags, &cli.StringFlag{
		Name:    "runner-key",
		Aliases: []string{"key"},
		Usage:   "Runner Key",
		EnvVars: []string{"RUNNER_KEY"},
	})

	app.Before = func(c *cli.Context) error {
		log.Printf("Using endpoint: %s\n", c.String("endpoint"))
		client = aoiclient.New(c.String("endpoint"))
		if c.String("runner-id") != "" || c.String("runner-key") != "" {
			client.Authenticate(c.String("runner-id"), c.String("runner-key"))
		}
		return nil
	}

	registerCommand(app)
	pollCommand(app)

	err := app.Run(os.Args)
	if err != nil {
		log.Fatalln(err)
	}

}
