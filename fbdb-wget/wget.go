package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
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
	"github.com/schollz/pluck/pluck"
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
		cli.StringSliceFlag{Name: "headers,H", Usage: "headers to include"},
		cli.BoolFlag{Name: "tor"},
		cli.BoolFlag{Name: "no-clobber,nc"},
		cli.StringFlag{Name: "list,i"},
		cli.StringFlag{Name: "pluck,p", Usage: "file for plucking"},
		cli.StringFlag{Name: "cookies,c"},
		cli.BoolFlag{Name: "compress"},
		cli.BoolFlag{Name: "debug,d"},
		cli.BoolFlag{Name: "quiet,q"},
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
		} else if c.GlobalBool("quiet") {
			setLogLevel("error")
		} else {
			setLogLevel("info")
		}
		w.headers = c.GlobalStringSlice("headers")
		w.noClobber = c.GlobalBool("no-clobber")
		w.userTor = c.GlobalBool("tor")
		w.compressResults = c.GlobalBool("compress")
		w.numWorkers = c.GlobalInt("workers")
		w.cookies = c.GlobalString("cookies")
		if w.numWorkers < 1 {
			return errors.New("cannot have less than 1 worker")
		}
		if c.GlobalString("pluck") != "" {
			b, err := ioutil.ReadFile(c.GlobalString("pluck"))
			if err != nil {
				return err
			}
			w.pluckerTOML = string(b)
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
	cookies         string
	headers         []string
	compressResults bool
	numWorkers      int
	pluckerTOML     string
	torconnection   []*tor.Tor
	fs              *fbdb.FileSystem
}

type job struct {
	url string
}
type result struct {
	url string
	err error
}

func (w *wget) getURL(id int, jobs <-chan job, results chan<- result) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 20,
		},
		Timeout: 10 * time.Second,
	}
	defer func() {
		log.Debugf("worker %d finished", id)
	}()

RestartTor:
	if w.userTor {
		// keep trying until it gets on
		for {
			log.Debugf("starting tor in worker %d", id)
			// Wait at most a minute to start network and get
			dialCtx, dialCancel := context.WithTimeout(context.Background(), 3000*time.Hour)
			defer dialCancel()
			// Make connection
			dialer, err := w.torconnection[id].Dialer(dialCtx, nil)
			if err != nil {
				log.Warn(err)
				continue
			}
			httpClient.Transport = &http.Transport{
				DialContext:         dialer.DialContext,
				MaxIdleConnsPerHost: 20,
			}

			// Get /
			resp, err := httpClient.Get("http://icanhazip.com/")
			if err != nil {
				log.Warn(err)
				continue
			}

			body, err := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Warn(err)
				continue
			}
			log.Debugf("worker %d IP: %s", id, bytes.TrimSpace(body))
			break
		}
	}

	for j := range jobs {
		err := func(j job) (err error) {
			// check if valid url
			if !strings.Contains(j.url, "://") {
				j.url = "https://" + j.url
			}

			filename := strings.Split(j.url, "://")[1]
			// if no clobber, check if exists
			if w.noClobber {
				var exists bool
				exists, err = w.fs.Exists(filename)
				if err != nil {
					return
				}
				if exists {
					log.Infof("already saved %s", j.url)
					return nil
				}
			}

			// make request
			req, err := http.NewRequest("GET", j.url, nil)
			if err != nil {
				err = errors.Wrap(err, "bad request")
				return
			}
			if len(w.headers) > 0 {
				for _, header := range w.headers {
					if strings.Contains(header, ":") {
						hs := strings.Split(header, ":")
						req.Header.Set(strings.TrimSpace(hs[0]), strings.TrimSpace(hs[1]))
					}
				}
			}
			if w.cookies != "" {
				req.Header.Set("Cookie", w.cookies)
			}
			resp, err := httpClient.Do(req)
			if err != nil && resp == nil {
				err = errors.Wrap(err, "bad do")
				return
			}

			// check request's validity
			log.Debugf("%d requested %s: %d %s", id, j.url, resp.StatusCode, http.StatusText(resp.StatusCode))
			if resp.StatusCode == 503 || resp.StatusCode == 403 {
				err = fmt.Errorf("received %d code", resp.StatusCode)
				if w.userTor {
					err = errors.Wrap(err, "restart tor")
				}
				return
			}

			// read out body
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return
			}

			if w.pluckerTOML != "" {
				plucker, _ := pluck.New()
				r := bufio.NewReader(bytes.NewReader(body))
				err = plucker.LoadFromString(w.pluckerTOML)
				if err != nil {
					return err
				}
				err = plucker.PluckStream(r)
				if err != nil {
					return
				}
				body = []byte(plucker.ResultJSON())
				log.Debugf("body: %s", body)
				if !bytes.Contains(body, []byte("{")) {
					return fmt.Errorf("could not get anything")
				}
			}

			// save
			f, err := w.fs.NewFile(filename, body)
			if err != nil {
				return
			}
			err = w.fs.Save(f)
			if err == nil {
				log.Infof("saved %s", j.url)
			}
			return
		}(j)
		results <- result{
			url: j.url,
			err: err,
		}
		if err != nil && strings.Contains(err.Error(), "restart tor") {
			goto RestartTor
		}
	}
}

func (w *wget) cleanup(interrupted bool) {
	if w.userTor {
		for i := range w.torconnection {
			err := w.torconnection[i].Close()
			if err != nil {
				log.Errorf("problem closing tor connection %d: %s", i, err.Error())
			}
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

	if w.fs != nil {
		w.fs.Close()
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

	w.fs, err = fbdb.New("urls.db", fbdb.OptionCompress(w.compressResults))
	if err != nil {
		panic(err)
	}

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
		log.Debug("counting number of lines")
		numURLs, err = countLines(w.fileWithList)
		if err != nil {
			return
		}
		log.Debugf("found %d lines", numURLs)
	}

	jobs := make(chan job, numURLs)
	results := make(chan result, numURLs)

	for i := 0; i < w.numWorkers; i++ {
		go w.getURL(i, jobs, results)
	}

	// submit jobs
	numJobs := 1
	if w.fileWithList != "" {
		var file *os.File
		file, err = os.Open(w.fileWithList)
		if err != nil {
			return
		}

		scanner := bufio.NewScanner(file)
		numJobs = 0
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
	log.Debugf("waiting for %d jobs", numJobs)
	for i := 0; i < numJobs; i++ {
		a := <-results
		if a.err != nil {
			log.Warnf("problem with %s: %s", a.url, a.err.Error())
		}
	}

	return
}
