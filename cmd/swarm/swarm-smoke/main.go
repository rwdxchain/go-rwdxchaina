// Copyright 2018 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"os"
	"sort"

	"github.com/rwdxchain/go-rwdxchaina/log"
	colorable "github.com/mattn/go-colorable"

	cli "gopkg.in/urfave/cli.v1"
)

var (
	endpoints        []string
	includeLocalhost bool
	cluster          string
	scheme           string
	filesize         int
	from             int
	to               int
)

func main() {
	log.PrintOrigins(true)
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(colorable.NewColorableStderr(), log.TerminalFormat(true))))

	app := cli.NewApp()
	app.Name = "smoke-test"
	app.Usage = ""

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "cluster-endpoint",
			Value:       "testing",
			Usage:       "cluster to point to (open, or testing)",
			Destination: &cluster,
		},
		cli.IntFlag{
			Name:        "cluster-from",
			Value:       8501,
			Usage:       "swarm node (from)",
			Destination: &from,
		},
		cli.IntFlag{
			Name:        "cluster-to",
			Value:       8512,
			Usage:       "swarm node (to)",
			Destination: &to,
		},
		cli.StringFlag{
			Name:        "cluster-scheme",
			Value:       "http",
			Usage:       "http or https",
			Destination: &scheme,
		},
		cli.BoolFlag{
			Name:        "include-localhost",
			Usage:       "whether to include localhost:8500 as an endpoint",
			Destination: &includeLocalhost,
		},
		cli.IntFlag{
			Name:        "filesize",
			Value:       1,
			Usage:       "file size for generated random file in MB",
			Destination: &filesize,
		},
	}

	app.Commands = []cli.Command{
		{
			Name:    "upload_and_sync",
			Aliases: []string{"c"},
			Usage:   "upload and sync",
			Action:  cliUploadAndSync,
		},
	}

	sort.Sort(cli.FlagsByName(app.Flags))
	sort.Sort(cli.CommandsByName(app.Commands))

	err := app.Run(os.Args)
	if err != nil {
		log.Error(err.Error())
	}
}
