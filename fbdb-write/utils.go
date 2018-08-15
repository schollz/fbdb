package main

import (
	"time"

	"github.com/teris-io/shortid"
)

func getUUID() string {
	sid, err := shortid.New(1, shortid.DefaultABC, uint64(time.Now().Nanosecond()))
	if err != nil {
		panic(err)
	}
	s, err := sid.Generate()
	if err != nil {
		panic(err)
	}
	return s
}
