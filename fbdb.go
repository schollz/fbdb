package fbdb

import (
	"bytes"
	"compress/flate"
	"database/sql"
	"io"
	"os"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"github.com/schollz/sqlite3dump"
)

type FileSystem struct {
	// options
	name              string
	readOnly          bool
	encryptPassphrase string
	doCompression     bool

	db *sql.DB
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

// Open will open  a filesystem-based database
func Open(name string, options ...Option) (fs *FileSystem, err error) {
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
		}
	} else {
		fs.db, err = sql.Open("sqlite3", fs.name)
	}

	return
}

// Close will close the database
func (fs *FileSystem) Close() (err error) {
	return fs.db.Close()
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
	dumpFile.Close()
	return
}

// NewFile returns a new file
func (fs *FileSystem) NewFile(name string, data []byte) (f File, err error) {
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

// GetI returns the ith thing
func (fs *FileSystem) GetI(i int) (f File, err error) {
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

// Len returns a number for a query, typically "SELECT COUNT(name) FROM fs"
func (fs *FileSystem) Len(queryCustom ...string) (l int, err error) {
	// prepare statement
	query := "SELECT COUNT(name) FROM FS"
	if len(queryCustom) > 0 {
		query = queryCustom[0]
	}
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

func ProcessFile(f *File) (err error) {
	if f.IsCompressed {
		f.Data = decompressByte(f.Data)
		f.Size = len(f.Data)
	}

	// TODO
	// decryption
	return
}

// Exists returns whether specified file exists
func (fs *FileSystem) Exists(name string) (exists bool, err error) {
	fs.Lock()
	defer fs.Unlock()
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

// GetAll pipes all the query into a function until the function
// returns true to stop
func (fs *FileSystem) GetAll(getfile func(f File) bool, query string, args ...interface{}) (err error) {
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
		if f.IsCompressed {
			f.Data = decompressByte(f.Data)
			f.Size = len(f.Data)
		}
		if getfile(f) {
			return
		}
	}
	err = rows.Err()
	if err != nil {
		err = errors.Wrap(err, "getRows")
	}
	return
}

// CustomGet will get any query and emit a function with it
func (fs *FileSystem) Pipeline(done <-chan struct{}, query string, args ...interface{}) (<-chan File, <-chan error) {
	out := make(chan File)
	outerr := make(chan error)

	go func() {
		// defer func() {
		// 	fmt.Println("closed")
		// }()
		defer close(out)
		defer close(outerr)
		// prepare statement
		stmt, err := fs.db.Prepare(query)
		if err != nil {
			outerr <- errors.Wrap(err, "preparing query: "+query)
			return
		}

		defer stmt.Close()
		rows, err := stmt.Query(args...)
		if err != nil {
			outerr <- errors.Wrap(err, query)
			return
		}

		// loop through rows
		defer rows.Close()
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
				outerr <- errors.Wrap(err, "getRows")
				return
			}
			if f.IsCompressed {
				f.Data = decompressByte(f.Data)
				f.Size = len(f.Data)
			}

			select {
			case out <- f:
			case <-done:
				// fmt.Println("returning early")
				return
			}
		}
		err = rows.Err()
		if err != nil {
			outerr <- errors.Wrap(err, "getRows")
		}

	}()

	return out, outerr
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

// compressByte returns a compressed byte slice.
func compressByte(src []byte) []byte {
	compressedData := new(bytes.Buffer)
	compress(src, compressedData, 9)
	return compressedData.Bytes()
}

// decompressByte returns a decompressed byte slice.
func decompressByte(src []byte) []byte {
	compressedData := bytes.NewBuffer(src)
	deCompressedData := new(bytes.Buffer)
	decompress(compressedData, deCompressedData)
	return deCompressedData.Bytes()
}

// compress uses flate to compress a byte slice to a corresponding level
func compress(src []byte, dest io.Writer, level int) {
	compressor, _ := flate.NewWriter(dest, level)
	compressor.Write(src)
	compressor.Close()
}

// compress uses flate to decompress an io.Reader
func decompress(src io.Reader, dest io.Writer) {
	decompressor := flate.NewReader(src)
	io.Copy(dest, decompressor)
	decompressor.Close()
}
