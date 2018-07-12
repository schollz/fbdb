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

type FileSystem struct {
	// options
	name              string
	readOnly          bool
	encryptPassphrase string
	doCompression     bool

	db       *sql.DB
	lock     *golock.Lock
	isOpen   bool
	isClosed bool
}

type File struct {
	// file meta data
	Permissions os.FileMode
	User        string
	Group       string
	Size        int
	Created     time.Time
	Modified    time.Time
	Name        string
	Data        []byte

	// file system data
	IsCompressed bool
	IsEncrypted  bool
}

// Option is the type all options need to adhere to
type Option func(fs *FileSystem)

// OptionReadOnly sets twhether to open as read only
func OptionReadOnly(readOnly bool) Option {
	return func(fs *FileSystem) {
		fs.readOnly = readOnly
	}
}

// New will initialize a filesystem
func New(name string, optoins ...Option) (fs *FileSystem, err error) {
	fs = new(FileSystem)
	if name == "" {
		return errors.New("database must have name")
	}
	fs.name = name

	for _, o := range options {
		o(l)
	}

	// if read-only, make sure the database exists
	if _, err = os.Stat(d.name); err != nil && fs.readOnly {
		err = errors.New("cannot open as read-only if it does not exist")
		return
	}

	fs.lock = golock.New(
		golock.OptionSetName(fs.name+".lock"),
		golockOptionSetInterval(1*time.Millisecond),
		OptionSetTimeout(30*time.Second),
	)

	return
}

func (fs *FileSystem) finishTransaction() (err error) {
	err = fs.db.Close()
	if err != nil {
		fs.lock.Unlock()
		return
	}
	err = fs.lock.Unlock()
	return
}

func (fs *FileSystem) startTransaction() (err error) {
	// obtain a lock on the database
	err = fs.lock.Lock()
	if err != nil {
		return
	}

	// check if it is a new database
	newDatabase := false
	if _, err := os.Stat(d.name); os.IsNotExist(err) {
		newDatabase = true
	}

	// open sqlite3 database
	fs.db, err = sql.Open("sqlite3", d.name)
	if err != nil {
		return
	}

	// create new database tables if needed
	if newDatabase {
		err = fs.initializeDB()
		if err != nil {
			return
		}
	}
	return
}

// File is the basic unit that is saved
type File struct {
	// file meta data
	Name        string
	Permissions os.FileMode
	User        string
	Group       string
	Size        int
	Created     time.Time
	Modified    time.Time
	Data        []byte

	// file system data
	IsCompressed bool
	IsEncrypted  bool
}

func (fs *FileSystem) initializeDB() (err error) {
	sqlStmt := `CREATE TABLE 
		fs (
			name TEXT NOT NULL PRIMARY KEY, 
			permissions INTEGER,
			user TEXT,
			group TEXT,
			size INTEGER,
			created TIMESTAMP,
			modified TIMESTAMP,
			data BLOB,
			compressed INTEGER,
			encrypted INTEGER
		);`
	_, err = fs.db.Exec(sqlStmt)
	if err != nil {
		err = errors.Wrap(err, "creating table")
	}
	return
}
