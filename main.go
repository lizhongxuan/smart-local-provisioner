package main

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/urfave/cli"
	"os"
)

var (
	VERSION = "0.0.1"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	a := cli.NewApp()
	a.Version = VERSION
	a.Usage = "Smart Local Provisioner"

	a.Before = func(c *cli.Context) error {
		if c.GlobalBool("debug") {
			logrus.SetLevel(logrus.DebugLevel)
		}
		return nil
	}

	a.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:   "debug, d",
			Usage:  "enable debug logging level",
			EnvVar: "SMART_LOCAL_DEBUG",
		},
	}
	a.Commands = []cli.Command{
		StartCmd(),
	}
	a.CommandNotFound = cmdNotFound
	a.OnUsageError = onUsageError

	if err := a.Run(os.Args); err != nil {
		logrus.Fatalf("Critical error: %v", err)
	}
}

func cmdNotFound(c *cli.Context, command string) {
	panic(fmt.Errorf("Unrecognized command: %s", command))
}

func onUsageError(c *cli.Context, err error, isSubcommand bool) error {
	panic(fmt.Errorf("Usage error, please check your command"))
}
