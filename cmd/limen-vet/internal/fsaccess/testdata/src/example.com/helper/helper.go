package helper

import (
	"io"
	"os"
)

func LoadFile(path string) ([]byte, error) { // want LoadFile:`reaches\(os.Open\)`
	f, err := os.Open(path) // want `library code touches the file system: os.Open`
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

func Parse(r io.Reader) ([]byte, error) { return io.ReadAll(r) }

func load(path string) ([]byte, error) { return os.ReadFile(path) } // want load:`reaches\(os.ReadFile\)` `os.ReadFile`

func ReadConfig() ([]byte, error) { return load("config") } // want ReadConfig:`reaches\(os.ReadFile\)`

type Store struct{}

func (*Store) Save(path string, b []byte) error { return os.WriteFile(path, b, 0o600) } // want Save:`reaches\(os.WriteFile\)` `os.WriteFile`

type Opener interface {
	Open(name string) (io.ReadCloser, error)
}

func Remove[T any](path string) T { // want Remove:`reaches\(os.Remove\)`
	_ = os.Remove(path) // want `os.Remove`

	var zero T

	return zero
}
