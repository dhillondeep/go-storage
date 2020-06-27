package storage_test

import (
	"testing"
	"time"

	storage "github.com/dhillondeep/go-storage/pkg"
	"github.com/stretchr/testify/assert"
)

const slowDelay = 400 * time.Millisecond

func withSlowWrapper(read time.Duration, write time.Duration, cb func(storage.FS)) {
	cb(storage.NewSlowWrapper(storage.NewMemoryFS(), read, write))
}

func TestNewSlowWrapper(t *testing.T) {
	withSlowWrapper(slowDelay, slowDelay, func(fs storage.FS) {
		start := time.Now()

		// testCreate will do a Create, Open, and Attributes, so 3 calls
		testCreate(t, fs, "foo", "bar")
		assert.WithinDuration(t, start.Add(slowDelay*3), time.Now(), slowDelay)
	})
}
