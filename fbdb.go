package fbdb

import (
	"database/sql"
	"os"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"github.com/schollz/golock"
	"github.com/schollz/sqlite3dump"
)

type FileSystem struct {
	// options
	name              string
	readOnly          bool
	encryptPassphrase string
	doCompression     bool

	db         *sql.DB
	dbReadonly *sql.DB
	filelock   *golock.Lock
	isLocked   bool
	sync.RWMutex
}

// File is the basic unit that is saved
type File struct {
	// file meta data
	Name        string
	Permissions os.FileMode
	User        string
	UserID      int
	Group       string
	GroupID     int
	Size        int
	Created     time.Time
	Modified    time.Time
	Data        []byte

	// file system data
	IsCompressed bool
	IsEncrypted  bool
}

// Option is the type all options need to adhere to
type Option func(fs *FileSystem)

// OptionCompress sets compression on
func OptionCompress(compress bool) Option {
	return func(fs *FileSystem) {
		fs.doCompression = compress
	}
}

// New will initialize a filesystem
func New(name string, options ...Option) (fs *FileSystem, err error) {
	fs = new(FileSystem)
	if name == "" {
		err = errors.New("database must have name")
		return
	}
	fs.name = name

	for _, o := range options {
		o(fs)
	}

	// if read-only, make sure the database exists
	if _, errExists := os.Stat(fs.name); errExists != nil {
		fs.db, err = sql.Open("sqlite3", fs.name)
		if err != nil {
			return
		}
		err = fs.initializeDB()
		if err != nil {
			err = errors.Wrap(err, "could not initialize")
			return
		}
		fs.db.Close()
	}

	fs.filelock = golock.New(
		golock.OptionSetName(fs.name+".lock"),
		golock.OptionSetInterval(1*time.Millisecond),
		golock.OptionSetTimeout(30*time.Second),
	)
	return
}

func (fs *FileSystem) finishTransaction() (err error) {
	if fs.db != nil {
		fs.db.Close()
	}
	fs.filelock.Unlock()
	return
}

func (fs *FileSystem) startTransaction(readonly bool) (err error) {
	if !readonly {
		// obtain a lock on the database if we are going to be writing
		err = fs.filelock.Lock()
		if err != nil {
			err = errors.Wrap(err, "could not get lock")
			return
		}
	}

	// open sqlite3 database
	if readonly {
		fs.db, err = sql.Open("sqlite3", fs.name)
	} else {
		fs.db, err = sql.Open("sqlite3", fs.name)
	}
	if err != nil {
		err = errors.Wrap(err, "could not open sqlite3 db")
		return
	}

	return
}

func (fs *FileSystem) initializeDB() (err error) {
	sqlStmt := `CREATE TABLE 
		fs (
			name TEXT NOT NULL PRIMARY KEY, 
			permissions INTEGER,
			user_id INTEGER,
			group_id INTEGER,
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

	sqlStmt = `CREATE TABLE 
	users (
		id INTEGER NOT NULL PRIMARY KEY, 
		name TEXT
	);`
	_, err = fs.db.Exec(sqlStmt)
	if err != nil {
		err = errors.Wrap(err, "creating table")
	}

	sqlStmt = `CREATE TABLE 
groups (
	id INTEGER NOT NULL PRIMARY KEY, 
	name TEXT
);`
	_, err = fs.db.Exec(sqlStmt)
	if err != nil {
		err = errors.Wrap(err, "creating table")
	}
	return
}

// DumpSQL will dump the SQL as text to filename.sql
func (fs *FileSystem) DumpSQL() (err error) {
	fs.Lock()
	defer fs.Unlock()
	var dumpFile *os.File
	dumpFile, err = os.Create(fs.name + ".sql")
	if err != nil {
		return
	}
	err = sqlite3dump.Dump(fs.name, dumpFile)
	return
}

// NewFile returns a new file
func (fs *FileSystem) NewFile(name string, data []byte) (f File, err error) {
	fs.Lock()
	defer fs.Unlock()
	f = File{
		Name:        name,
		Permissions: os.FileMode(0644),
		Size:        len(data),
		Created:     time.Now(),
		Modified:    time.Now(),
		Data:        data,
	}

	if fs.doCompression {
		f.IsCompressed = true
		f.Data = compressByte(data)
		f.Size = len(f.Data)
	}

	if fs.encryptPassphrase != "" {
		f.IsEncrypted = true
		// TODO: do encryption
	}
	return
}

// Save a file to the file system
func (fs *FileSystem) Save(f File) (err error) {
	fs.Lock()
	defer fs.Unlock()

	defer fs.finishTransaction()
	err = fs.startTransaction(false)
	if err != nil {
		return
	}

	tx, err := fs.db.Begin()
	if err != nil {
		return errors.Wrap(err, "begin Save")
	}

	stmt, err := tx.Prepare(`
	INSERT OR REPLACE INTO
		fs
	(
		name, 
		permissions,
		user_id,
		group_id,
		size,
		created,
		modified,
		data,
		compressed,
		encrypted
	) 
		values 	
	(
		?, 
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?
	)`)
	if err != nil {
		return errors.Wrap(err, "stmt Save")
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		f.Name,
		f.Permissions,
		f.UserID,
		f.GroupID,
		f.Size,
		f.Created,
		f.Modified,
		f.Data,
		f.IsCompressed,
		f.IsEncrypted,
	)
	if err != nil {
		return errors.Wrap(err, "exec Save")
	}
	err = tx.Commit()
	if err != nil {
		return errors.Wrap(err, "commit Save")
	}
	return

}

// Close will make sure that the lock file is closed
func (fs *FileSystem) Close() (err error) {
	fs.Lock()
	defer fs.Unlock()

	return fs.finishTransaction()
}

// GetI returns the ith thing
func (fs *FileSystem) GetI(i int) (f File, err error) {
	fs.Lock()
	defer fs.Unlock()

	defer fs.finishTransaction()
	err = fs.startTransaction(true)
	if err != nil {
		return
	}

	files, err := fs.getAllFromPreparedQuery(`
		SELECT * FROM fs LIMIT 1 OFFSET ?`, i)
	if err != nil {
		err = errors.Wrap(err, "Stat")
	}
	if len(files) == 0 {
		err = errors.New("no files")
	} else {
		f = files[0]
	}

	if f.IsCompressed {
		f.Data = decompressByte(f.Data)
		f.Size = len(f.Data)
	}
	return
}

func (fs *FileSystem) Len() (l int, err error) {
	fs.Lock()
	defer fs.Unlock()

	defer fs.finishTransaction()
	err = fs.startTransaction(true)
	if err != nil {
		return
	}
	// prepare statement
	query := "SELECT COUNT(name) FROM FS"
	stmt, err := fs.db.Prepare(query)
	if err != nil {
		err = errors.Wrap(err, "preparing query: "+query)
		return
	}

	defer stmt.Close()
	rows, err := stmt.Query()
	if err != nil {
		err = errors.Wrap(err, query)
		return
	}

	// loop through rows
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&l)
		if err != nil {
			err = errors.Wrap(err, "getRows")
			return
		}
	}
	err = rows.Err()
	if err != nil {
		err = errors.Wrap(err, "getRows")
	}
	return
}

// Get returns the info from a file
func (fs *FileSystem) Get(name string) (f File, err error) {
	fs.Lock()
	defer fs.Unlock()

	defer fs.finishTransaction()
	err = fs.startTransaction(true)
	if err != nil {
		return
	}

	files, err := fs.getAllFromPreparedQuery(`
		SELECT * FROM fs WHERE name = ?`, name)
	if err != nil {
		err = errors.Wrap(err, "Stat")
	}
	if len(files) == 0 {
		err = errors.New("no files with that name")
	} else {
		f = files[0]
	}

	ProcessFile(&f)

	return
}

func ProcessFile(f *File) {
	if f.IsCompressed {
		f.Data = decompressByte(f.Data)
		f.Size = len(f.Data)
	}

	// TODO
	// decryption

}

// Exists returns whether specified file exists
func (fs *FileSystem) Exists(name string) (exists bool, err error) {
	fs.Lock()
	defer fs.Unlock()

	defer fs.finishTransaction()
	err = fs.startTransaction(true)
	if err != nil {
		return
	}

	files, err := fs.getAllFromPreparedQuery(`
		SELECT * FROM fs WHERE name = ?`, name)
	if err != nil {
		err = errors.Wrap(err, "Exists")
	}
	if len(files) > 0 {
		exists = true
	}
	return
}

func (fs *FileSystem) getAllFromPreparedQuery(query string, args ...interface{}) (files []File, err error) {
	// prepare statement
	stmt, err := fs.db.Prepare(query)
	if err != nil {
		err = errors.Wrap(err, "preparing query: "+query)
		return
	}

	defer stmt.Close()
	rows, err := stmt.Query(args...)
	if err != nil {
		err = errors.Wrap(err, query)
		return
	}

	// loop through rows
	defer rows.Close()
	files = []File{}
	for rows.Next() {
		var f File
		err = rows.Scan(
			&f.Name,
			&f.Permissions,
			&f.UserID,
			&f.GroupID,
			&f.Size,
			&f.Created,
			&f.Modified,
			&f.Data,
			&f.IsCompressed,
			&f.IsEncrypted,
		)
		if err != nil {
			err = errors.Wrap(err, "getRows")
			return
		}
		files = append(files, f)
	}
	err = rows.Err()
	if err != nil {
		err = errors.Wrap(err, "getRows")
	}
	return
}

func (fs *FileSystem) ManualStart() (err error) {
	fs.db, err = sql.Open("sqlite3", fs.name)
	if err != nil {
		err = errors.Wrap(err, "could not open sqlite3 db")
		return
	}
	return
}

func (fs *FileSystem) ManualStop() (err error) {
	return fs.db.Close()
}

func (fs *FileSystem) ManualGet(name string) (f File, err error) {
	files, err := fs.getAllFromPreparedQuery(`
		SELECT * FROM fs WHERE name = ?`, name)
	if err != nil {
		err = errors.Wrap(err, "Stat")
	}
	if len(files) == 0 {
		err = errors.New("no files with that name")
	} else {
		f = files[0]
	}

	if f.IsCompressed {
		f.Data = decompressByte(f.Data)
		f.Size = len(f.Data)
	}
	return
}

func (fs *FileSystem) ManualGetI(i int) (f File, err error) {
	files, err := fs.getAllFromPreparedQuery(`SELECT * FROM fs LIMIT 1 OFFSET ?`, i)
	if err != nil {
		err = errors.Wrap(err, "Stat")
	}
	if len(files) == 0 {
		err = errors.New("no files")
	} else {
		f = files[0]
	}

	if f.IsCompressed {
		f.Data = decompressByte(f.Data)
		f.Size = len(f.Data)
	}
	return
}
