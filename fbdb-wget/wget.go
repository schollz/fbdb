package main

import (
	"bufio"
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/cretz/bine/tor"
	"github.com/pkg/errors"
	"github.com/schollz/fbdb"
	"github.com/urfave/cli"
)

func main() {
	defer log.Flush()

	app := cli.NewApp()
	app.Name = "fbdb-wget"
	app.Version = "0.1.0"
	app.Compiled = time.Now()
	app.HelpName = "fbdb-wget help"
	app.Usage = "similar to wget, but write to database"
	app.UsageText = "fbdb-wget [options] [url]"
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "tor"},
		cli.BoolFlag{Name: "no-clobber,nc"},
		cli.StringFlag{Name: "list,i"},
		cli.BoolFlag{Name: "compress"},
		cli.BoolFlag{Name: "debug,d"},
		cli.IntFlag{Name: "workers,w", Value: 1},
	}
	app.Action = func(c *cli.Context) error {
		w := new(wget)
		if c.Args().First() != "" {
			w.url = c.Args().First()
		} else if c.GlobalString("list") != "" {
			w.fileWithList = c.GlobalString("list")
		} else {
			return errors.New("need to specify URL")
		}
		if c.GlobalBool("debug") {
			setLogLevel("debug")
		} else {
			setLogLevel("info")
		}
		w.noClobber = c.GlobalBool("no-clobber")
		w.userTor = c.GlobalBool("tor")
		w.compressResults = c.GlobalBool("compress")
		w.numWorkers = c.GlobalInt("workers")
		if w.numWorkers < 1 {
			return errors.New("cannot have less than 1 worker")
		}
		return w.start()
	}

	// ignore error so we don't exit non-zero and break gfmrun README example tests
	err := app.Run(os.Args)
	if err != nil {
		log.Error(err)
	}
}

type wget struct {
	userTor         bool
	noClobber       bool
	fileWithList    string
	url             string
	compressResults bool
	numWorkers      int
	torconnection   []*tor.Tor
}

type job struct {
	url string
}
type result struct {
	url string
	err error
}

func (w *wget) getURL(id int, jobs <-chan job, results chan<- result) {
	fs, err := fbdb.New("urls.db", fbdb.OptionCompress(w.compressResults))
	if err != nil {
		panic(err)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 20,
		},
		Timeout: 10 * time.Second,
	}
	defer func() {
		log.Debugf("worker %d finished", id)
	}()
	if w.userTor {
		log.Debugf("starting tor in worker %d", id)
		// Wait at most a minute to start network and get
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 3000*time.Hour)
		defer dialCancel()
		// Make connection
		dialer, err := w.torconnection[id].Dialer(dialCtx, nil)
		if err != nil {
			log.Error(err)
			return
		}
		httpClient.Transport = &http.Transport{DialContext: dialer.DialContext}

		// Get /
		resp, err := httpClient.Get("http://icanhazip.com/")
		if err != nil {
			log.Error(err)
			return
		}

		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("your new IP: %s", bytes.TrimSpace(body))
	}

	for j := range jobs {
		err := func(j job) (err error) {
			// check if valid url
			if !strings.Contains(j.url, "://") {
				err = errors.New("malformed url")
				return
			}

			filename := strings.Split(j.url, "://")[1]
			// if no clobber, check if exists
			if w.noClobber {
				var exists bool
				exists, err = fs.Exists(filename)
				if err != nil {
					return
				}
				if exists {
					log.Infof("already saved %s", j.url)
					return nil
				}
			}

			// make request
			log.Debugf("making request for %s", j.url)
			req, err := http.NewRequest("GET", j.url, nil)
			if err != nil {
				err = errors.Wrap(err, "bad request")
				return
			}
			resp, err := httpClient.Do(req)
			if err != nil && resp == nil {
				err = errors.Wrap(err, "bad do")
				return
			}

			// read out body
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return
			}

			// save
			f, err := fs.NewFile(filename, body)
			if err != nil {
				return
			}
			err = fs.Save(f)
			if err == nil {
				log.Infof("saved %s", j.url)
			}
			return
		}(j)
		results <- result{
			url: j.url,
			err: err,
		}
	}
}

func (w *wget) cleanup(interrupted bool) {
	if w.userTor {
		for i := range w.torconnection {
			w.torconnection[i].Close()
		}

		torFolders, err := filepath.Glob("data-dir-*")
		if err != nil {
			log.Error(err)
			return
		}
		for _, torFolder := range torFolders {
			errRemove := os.RemoveAll(torFolder)
			if errRemove == nil {
				log.Debugf("removed %s", torFolder)
			}
		}
	}

	if interrupted {
		os.Exit(1)
	}
}

func (w *wget) start() (err error) {
	defer log.Flush()
	defer w.cleanup(false)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			// cleanup
			log.Debug(sig)
			w.cleanup(true)
		}
	}()

	if w.userTor {
		w.torconnection = make([]*tor.Tor, w.numWorkers)
		for i := 0; i < w.numWorkers; i++ {
			w.torconnection[i], err = tor.Start(nil, nil)
			if err != nil {
				return
			}
		}
	}

	numURLs := 1
	if w.fileWithList != "" {
		numURLs, err = countLines(w.fileWithList)
		if err != nil {
			return
		}
	}

	jobs := make(chan job, numURLs)
	results := make(chan result, numURLs)

	for i := 0; i < w.numWorkers; i++ {
		go w.getURL(i, jobs, results)
	}

	// submit jobs
	if w.fileWithList != "" {
		var file *os.File
		file, err = os.Open(w.fileWithList)
		if err != nil {
			return
		}

		scanner := bufio.NewScanner(file)
		numJobs := 0
		for scanner.Scan() {
			numJobs++
			jobs <- job{
				url: strings.TrimSpace(scanner.Text()),
			}
		}
		log.Debugf("sent %d jobs", numJobs)

		if errScan := scanner.Err(); errScan != nil {
			log.Error(errScan)
		}
		file.Close()
	} else {
		jobs <- job{
			url: w.url,
		}
	}
	close(jobs)

	// print out errors
	log.Debugf("waiting for %d jobs", numURLs)
	for i := 0; i < numURLs; i++ {
		a := <-results
		if a.err != nil {
			log.Warnf("problem with %s: %s", a.url, a.err.Error())
		}
	}

	return
}
