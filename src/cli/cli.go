package cli

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/schollz/fbdb/src/fbdb"
	"github.com/schollz/fbdb/src/get"
	"github.com/schollz/progressbar/v2"
	"github.com/urfave/cli"
)

func init() {
	setLogLevel("debug")
}

// SetLogLevel determines the log level
func setLogLevel(level string) (err error) {

	// https://en.wikipedia.org/wiki/ANSI_escape_code#3/4_bit
	// https://github.com/cihub/seelog/wiki/Log-levels
	appConfig := `
	<seelog minlevel="` + level + `">
	<outputs formatid="stdout">
	<filter levels="debug,trace">
		<console formatid="debug"/>
	</filter>
	<filter levels="info">
		<console formatid="info"/>
	</filter>
	<filter levels="critical,error">
		<console formatid="error"/>
	</filter>
	<filter levels="warn">
		<console formatid="warn"/>
	</filter>
	</outputs>
	<formats>
		<format id="stdout"   format="%Date %Time [%LEVEL] %File %FuncShort:%Line %Msg %n" />
		<format id="debug"   format="%Date %Time %EscM(37)[%LEVEL]%EscM(0) %File %FuncShort:%Line %Msg %n" />
		<format id="info"    format="%EscM(36)[%LEVEL]%EscM(0) %Msg %n" />
		<format id="warn"    format="%EscM(33)[%LEVEL]%EscM(0) %Msg %n" />
		<format id="error"   format="%EscM(31)[%LEVEL]%EscM(0) %Msg %n" />
	</formats>
	</seelog>
	`
	logger, err := log.LoggerFromConfigAsBytes([]byte(appConfig))
	if err != nil {
		return
	}
	log.ReplaceLogger(logger)
	return
}

func Run() (err error) {
	defer log.Flush()

	app := cli.NewApp()
	app.Name = "fbdb"
	app.Version = "0.1.0"
	app.Compiled = time.Now()
	app.Usage = "easily and securely transfer stuff from one computer to another"
	app.UsageText = "croc allows any two computers to directly and securely transfer files"
	app.Commands = []cli.Command{
		{
			Name:        "get",
			Usage:       "get some url(s)",
			Description: "running get will establish a connection with a website and download the websites into the database",
			ArgsUsage:   "[filename]",
			Flags: []cli.Flag{
				cli.StringSliceFlag{Name: "headers,H", Usage: "headers to include"},
				cli.BoolFlag{Name: "tor"},
				cli.BoolFlag{Name: "no-clobber,nc"},
				cli.StringFlag{Name: "list,i"},
				cli.StringFlag{Name: "pluck,p", Usage: "file for plucking"},
				cli.StringFlag{Name: "cookies,c"},
				cli.BoolFlag{Name: "compressed"},
				cli.BoolFlag{Name: "quiet,q"},
				cli.IntFlag{Name: "workers,w", Value: 1},
			},
			HelpName: "croc send",
			Action: func(c *cli.Context) error {
				return runget(c)
			},
		},
		{
			Name:        "dump",
			Description: "dump a database to folders",
			ArgsUsage:   "[database]",
			Action: func(c *cli.Context) error {
				return dump(c)
			},
			Flags: []cli.Flag{
				cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "ports of the relay"},
			},
		},
	}
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "debug", Usage: "increase verbosity"},
	}
	app.Before = func(c *cli.Context) error {
		if c.GlobalBool("debug") {
			setLogLevel("debug")
		} else {
			setLogLevel("warn")
		}
		return nil
	}

	// ignore error so we don't exit non-zero and break gfmrun README example tests
	return app.Run(os.Args)
}

func runget(c *cli.Context) (err error) {
	w := new(get.Get)
	if c.Args().First() != "" {
		w.URL = c.Args().First()
	} else if c.String("list") != "" {
		w.FileWithList = c.String("list")
	} else {
		return errors.New("need to specify URL")
	}
	if c.GlobalBool("debug") {
		setLogLevel("debug")
	} else if c.GlobalBool("quiet") {
		setLogLevel("error")
	} else {
		setLogLevel("info")
	}
	w.Headers = c.StringSlice("headers")
	w.NoClobber = c.Bool("no-clobber")
	w.UseTor = c.Bool("tor")
	w.CompressResults = c.Bool("compress")
	w.NumWorkers = c.Int("workers")
	w.Cookies = c.String("cookies")
	if w.NumWorkers < 1 {
		return errors.New("cannot have less than 1 worker")
	}
	if c.String("pluck") != "" {
		b, err := ioutil.ReadFile(c.String("pluck"))
		if err != nil {
			return err
		}
		w.PluckerTOML = string(b)
	}
	return w.Run()
}

func dump(c *cli.Context) (err error) {
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
	bar := progressbar.NewOptions(numFiles,
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
	)
	for i := 0; i < numFiles; i++ {
		bar.Add(1)
		var f fbdb.File
		f, err = fs.GetI(i)
		if err != nil {
			return
		}
		pathname, filename := path.Split(strings.TrimSuffix(strings.TrimSpace(f.Name), "/"))
		os.MkdirAll(pathname, 0755)
		err = ioutil.WriteFile(path.Join(pathname, filename), f.Data, 0644)
		if err != nil {
			log.Error(err)
			continue
		}
	}
	return
}
