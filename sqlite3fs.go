package sqlite3fs

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type database struct {
	name     string
	db       *sql.DB
	isOpen   bool
	isClosed bool
}

// New will open the database for transactions by first aquiring a filelock.
func New(name string, readOnly ...bool) (d *Database, err error) {
	db = new(database)
	db.name = strings.TrimSpace(family)

	// if read-only, make sure the database exists
	if _, err = os.Stat(d.name); err != nil && len(readOnly) > 0 && readOnly[0] {
		err = errors.New(fmt.Sprintf("group '%s' does not exist", d.family))
		return
	}

	// obtain a lock on the database
	// logger.Log.Debugf("getting filelock on %s", d.name+".lock")
	for {
		var ok bool
		databaseLock.Lock()
		if _, ok = databaseLock.Locked[d.name]; !ok {
			databaseLock.Locked[d.name] = true
		}
		databaseLock.Unlock()
		if !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// logger.Log.Debugf("got filelock")

	// check if it is a new database
	newDatabase := false
	if _, err := os.Stat(d.name); os.IsNotExist(err) {
		newDatabase = true
	}

	// open sqlite3 database
	d.db, err = sql.Open("sqlite3", d.name)
	if err != nil {
		return
	}
	// logger.Log.Debug("opened sqlite3 database")

	// create new database tables if needed
	if newDatabase {
		err = d.MakeTables()
		if err != nil {
			return
		}
		logger.Log.Debug("made tables")
	}

	return
}
