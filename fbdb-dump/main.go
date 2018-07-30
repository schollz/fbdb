package main

import (
	"io/ioutil"
	"os"
	"path"
	"time"

	log "github.com/cihub/seelog"
	"github.com/schollz/fbdb"
	"github.com/urfave/cli"
)

func main() {
	defer log.Flush()

	app := cli.NewApp()
	app.Name = "fbdb-dump"
	app.Version = "0.1.0"
	app.Compiled = time.Now()
	app.HelpName = "fbdb-dump help"
	app.UsageText = "fbdb-dump [options] [url]"
	app.Flags = []cli.Flag{}
	app.Action = func(c *cli.Context) (err error) {
		_, err = os.Stat(c.Args().First())
		if err != nil {
			return
		}
		fs, err := fbdb.New(c.Args().First())
		if err != nil {
			return
		}
		numFiles, err := fs.Len()
		if err != nil {
			return
		}
		for i := 0; i < numFiles; i++ {
			var f fbdb.File
			f, err = fs.GetI(i)
			if err != nil {
				return
			}
			pathname, filename := path.Split(f.Name)
			os.MkdirAll(pathname, 0755)
			log.Debug(pathname, filename)
			err = ioutil.WriteFile(path.Join(pathname, filename), f.Data, 0644)
			if err != nil {
				return
			}
		}
		return
	}

	// ignore error so we don't exit non-zero and break gfmrun README example tests
	err := app.Run(os.Args)
	if err != nil {
		log.Error(err)
	}
}
