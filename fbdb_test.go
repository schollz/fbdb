package fbdb

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func BenchmarkNewFile(b *testing.B) {
	os.Remove("test.db")
	os.Remove("test.db.lock")
	defer os.Remove("test.db")
	fs, _ := Open("test.db")
	for i := 0; i < b.N; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}
}

func BenchmarkGet(b *testing.B) {
	os.Remove("test.db")
	os.Remove("test.db.lock")
	defer os.Remove("test.db")
	fs, _ := Open("test.db")
	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.Get("test99")
	}
}
func BenchmarkGet2(b *testing.B) {
	os.Remove("test.db")
	os.Remove("test.db.lock")
	defer os.Remove("test.db")
	fs, _ := Open("test.db")
	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.getAllFromPreparedQuery(`
		SELECT * FROM fs WHERE name = ?`, "test99")
	}
}

func TestPipelineCanonical(t *testing.T) {
	os.Remove("test.db")
	fs, _ := Open("test.db")
	defer fs.Close()

	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}

	done := make(chan struct{})
	files, errchan := fs.Pipeline(done, "SELECT * FROM fs LIMIT 10")
	defer func() {
		close(done)
	}()
	count := 0
L:
	for {
		select {
		case f, more := <-files:
			if !more {
				break L
			} else {
				assert.True(t, strings.HasPrefix(f.Name, "test"))
				count++
			}
		case err := <-errchan:
			assert.Nil(t, err)
			break L
		}
	}
	assert.Equal(t, 10, count)
}

func TestGetAll(t *testing.T) {
	fs, _ := Open("test.db")
	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}

	err0 := fs.GetAll(func(f File) bool {
		fmt.Println(f)
		if f.Name == "test3" {
			return true // will stop
		}
		return false
	}, "SELECT * FROM fs")
	assert.Nil(t, err0)
}

func TestPipelineError(t *testing.T) {
	fs, _ := Open("test.db")
	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}

	done := make(chan struct{})
	files, errchan := fs.Pipeline(done, "bad sql")
	defer func() {
		close(done)
	}()
	var err0 error
	for {
		finished := false
		select {
		case _, more := <-files:
			if !more {
				finished = true
			}
		case err0 = <-errchan:
			finished = true
		}
		if finished {
			break
		}
	}
	assert.NotNil(t, err0)
}
func TestPipelineInterrupt(t *testing.T) {
	fs, _ := Open("test.db")
	defer fs.Close()

	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}

	done := make(chan struct{})
	files, errchan := fs.Pipeline(done, "SELECT * FROM fs LIMIT 10")
	defer func() {
		close(done)
	}()
	count := 0
	for {
		finished := false
		select {
		case f, more := <-files:
			count++
			if f.Name == "test5" {
				done <- struct{}{} // NEED TO SPECIFY THAT WE ARE DONE EARLY
				finished = true
			}
			if !more {
				finished = true
			}
		case err := <-errchan:
			fmt.Println("error2", err)
			finished = true
		}
		if finished {
			break
		}
	}

	assert.Equal(t, 6, count)
	time.Sleep(100 * time.Millisecond)
}

func TestBasic(t *testing.T) {
	os.Remove("test.db")

	fs, err := Open("test.db")
	defer fs.Close()
	assert.Nil(t, err)

	f, err := fs.NewFile("test1", []byte("aslkdfjaklsdf"))
	assert.Nil(t, err)
	err = fs.Save(f)
	assert.Nil(t, err)

	f2, err := fs.Get("test1")
	assert.Equal(t, f.Data, f2.Data)
	assert.Nil(t, err)

	exists, err := fs.Exists("doesn't exist")
	assert.Nil(t, err)
	assert.False(t, exists)
	exists, err = fs.Exists("test1")
	assert.Nil(t, err)
	assert.True(t, exists)

	err = fs.DumpSQL()
	assert.Nil(t, err)
}

func TestConcurrency(t *testing.T) {
	fs, err := Open("test.db")
	assert.Nil(t, err)
	defer fs.Close()

	var wg sync.WaitGroup
	wg.Add(200)
	for i := 0; i < 200; i++ {
		go func(i int) {
			defer wg.Done()

			start := time.Now()
			for {
				if i < 100 {
					f, err := fs.NewFile(fmt.Sprintf("test%d", i), []byte("aslkdfjaklsdf"))
					assert.Nil(t, err)
					err = fs.Save(f)
				} else {
					f, err := fs.Get("test1")
					assert.Equal(t, "test1", f.Name)
					assert.Nil(t, err)
				}
				if time.Since(start) > 3*time.Second {
					break
				}
			}
		}(i)
	}
	wg.Wait()
}
