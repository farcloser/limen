package files

import "os"

// Only its importers report it: the reach crosses into lib.
func Read(path string) ([]byte, error) { return os.ReadFile(path) } // want Read:`reaches\(os.ReadFile\)`
