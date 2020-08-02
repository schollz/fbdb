# fbdb

[![travis](https://travis-ci.org/schollz/fbdb.svg?branch=master)](https://travis-ci.org/schollz/fbdb) 
[![go report card](https://goreportcard.com/badge/github.com/schollz/fbdb)](https://goreportcard.com/report/github.com/schollz/fbdb) 
[![coverage](https://img.shields.io/badge/coverage-84%25-brightgreen.svg)](https://gocover.io/github.com/schollz/fbdb)
[![godocs](https://godoc.org/github.com/schollz/fbdb?status.svg)](https://godoc.org/github.com/schollz/fbdb) 

*fbdb* is a *file-based database*. It is basically a SQL database that has file-like columns (Name, Date, Data) etc. It has some benefit to using a real filesystem for some instances, in that you can store millions of files without problems and you can also add compression.

## Install

```
$ go get -v github.com/schollz/fbdb
```

## Usage

You can insert data as "files":

```golang
fs, _ := fbdb.Open("new.db")
defer fs.Close()
f, _ := fs.NewFile("filename", []byte("data"))
fs.Save(f)
```

You can can retrieve files by name:

```golang
f, _ := fs.Get("filename")
```

See examples for more information.

## Contributing

Pull requests are welcome. Feel free to...

- Revise documentation
- Add new features
- Fix bugs
- Suggest improvements

## Thanks

Thanks Dr. H for the idea.

## License

MIT
