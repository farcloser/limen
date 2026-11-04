package lib

import (
	"io"
	"os"

	"example.com/helper"
	"example.com/lib/internal/files"
)

var open = os.Open // want `os.Open`

func A(path string) ([]byte, error) { // want A:`reaches\(os.Open\)`
	return helper.LoadFile(path) // want `library code calls helper.LoadFile, which touches the file system through os.Open`
}

func G() ([]byte, error) { return files.Read("x") } // want G:`reaches\(os.ReadFile\)` `calls files.Read, which touches the file system through os.ReadFile`

func B(r io.Reader) ([]byte, error) { return helper.Parse(r) }

func C(s *helper.Store) error { return s.Save("x", nil) } // want C:`reaches\(os.WriteFile\)` `calls \(\*helper.Store\).Save, which touches the file system through os.WriteFile`

func D(o helper.Opener) error {
	rc, err := o.Open("x")
	if err != nil {
		return err
	}

	return rc.Close()
}

func E() int { return helper.Remove[int]("x") } // want E:`reaches\(os.Remove\)` `calls helper.Remove`

func F() func(string) (*os.File, error) { return os.Create } // want F:`reaches\(os.Create\)` `os.Create`

func Stat() ([]byte, error) { // want Stat:`reaches\(os.ReadFile\)`
	//limen:allow fsaccess -- /proc is the kernel's interface, not a file a caller could hand over
	return os.ReadFile("/proc/self/stat")
}

func NoReason() error { // want NoReason:`reaches\(os.Stat\)`
	//limen:allow fsaccess // want `limen:allow fsaccess needs a reason`
	_, err := os.Stat("x") // want `os.Stat`

	return err
}

func Other() error {
	//limen:allow buildversion -- another analyzer's directive silences nothing here
	return nil
}
