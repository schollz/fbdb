package fbdb

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBasic(t *testing.T) {
	os.Remove("test.db")

	fs, err := New("test.db")
	assert.Nil(t, err)

	err = fs.startTransaction()
	assert.Nil(t, err)
	err = fs.finishTransaction()
	assert.Nil(t, err)

	f, err := fs.NewFile("test1", []byte("aslkdfjaklsdf"))
	assert.Nil(t, err)
	err = fs.Save(f)
	assert.Nil(t, err)

	f2, err := fs.Open("test1")
	assert.Equal(t, f.Data, f2.Data)
	assert.Nil(t, err)

	exists, err := fs.Exists("doesn't exist")
	assert.Nil(t, err)
	assert.False(t, exists)
	exists, err = fs.Exists("test1")
	assert.Nil(t, err)
	assert.True(t, exists)
}
