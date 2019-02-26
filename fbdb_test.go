package fbdb

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func BenchmarkNewFile(b *testing.B) {
	os.Remove("test.db")
	os.Remove("test.db.lock")
	defer os.Remove("test.db")
	fs, _ := New("test.db")
	for i := 0; i < b.N; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}
}

func BenchmarkGet(b *testing.B) {
	os.Remove("test.db")
	os.Remove("test.db.lock")
	defer os.Remove("test.db")
	fs, _ := New("test.db")
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
	fs, _ := New("test.db")
	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}
	fs.startTransaction(true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.getAllFromPreparedQuery(`
		SELECT * FROM fs WHERE name = ?`, "test99")
	}
	fs.finishTransaction()
}

func TestEmiting(t *testing.T) {
	fs, _ := New("test.db")
	for i := 0; i < 100; i++ {
		f, _ := fs.NewFile("test"+strconv.Itoa(i), []byte("aslkdfjaklsdf"))
		fs.Save(f)
	}
	files := make(chan File, 1)
	go func() {
		err := fs.GetChannel(files, "SELECT * FROM fs LIMIT ?", 50)
		assert.Nil(t, err)
	}()
	for {
		f, more := <-files
		if !more {
			break
		}
		fmt.Println(f)
	}
}

func TestBasic(t *testing.T) {
	os.Remove("test.db")

	fs, err := New("test.db")
	assert.Nil(t, err)

	err = fs.startTransaction(false)
	assert.Nil(t, err)
	err = fs.finishTransaction()
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
	var wg sync.WaitGroup
	wg.Add(200)
	for i := 0; i < 200; i++ {
		go func() {
			defer wg.Done()
			fs, err := New("test.db")
			assert.Nil(t, err)
			start := time.Now()
			for {
				_, err = fs.Get("test1")
				assert.Nil(t, err)
				if time.Since(start) > 1*time.Second {
					break
				}
			}
		}()
	}
	wg.Wait()
}
