package lib

import (
	"os"
	"testing"
)

func TestFixture(t *testing.T) {
	f, err := os.Open("testdata/x")
	if err == nil {
		f.Close()
	}
}
