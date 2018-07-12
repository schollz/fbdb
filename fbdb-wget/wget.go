package main

import (
	"bufio"
	"errors"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/schollz/fbdb"
	"github.com/urfave/cli"
)

func main() {
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
	}
	app.Action = func(c *cli.Context) error {
		w := new(wget)
		if c.Args().First() != "" {
			w.url = c.Args().First()
		}
		return w.start()
	}

	// ignore error so we don't exit non-zero and break gfmrun README example tests
	_ = app.Run(os.Args)
}

type wget struct {
	userTor      bool
	noClobber    bool
	fileWithList string
	url          string

	httpClient *http.Client
}

type job struct {
	url string
}
type result struct {
	url string
	err error
}

func (w *wget) getURL(id int, jobs <-chan job, results chan<- result) {
	fs, err := fbdb.New("urls.db")
	if err != nil {
		panic(err)
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
				_, errExists := fs.Open(filename)
				if errExists == nil {
					return nil
				}
			}

			// make request
			req, err := http.NewRequest("GET", j.url, nil)
			if err != nil {
				return
			}
			resp, err := w.httpClient.Do(req)
			if err != nil && resp == nil {
				return
			}

			// read out body
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return
			}
			log.Debugf("body: %s", body)

			// save
			f, err := fs.NewFile(filename, body)
			if err != nil {
				return
			}
			err = fs.Save(f)
			return
		}(j)
		results <- result{
			url: j.url,
			err: err,
		}
	}
}

func (w *wget) start() (err error) {
	defer log.Flush()
	w.httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 20,
		},
		Timeout: 10 * time.Second,
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

	for i := 0; i < 1; i++ {
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
		for scanner.Scan() {
			jobs <- job{
				url: strings.TrimSpace(scanner.Text()),
			}
		}

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
	for i := 0; i < numURLs; i++ {
		a := <-results
		if a.err == nil {
			log.Infof("saved %s", a.url)
		} else {
			log.Warnf("problem with %s: %s", a.url, a.err.Error())
		}
	}

	return
}
