package signers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// filePermissions is the mode of the file written: readable by all, as a
// tracked file is.
const filePermissions = 0o644

// Load reads the allowed-signers file at path. A file that is not there is
// an empty list: the first key added creates it.
func Load(path string) (List, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the file the caller designates, the command's contract.
	if errors.Is(err, fs.ErrNotExist) {
		return List{}, nil
	}

	if err != nil {
		return List{}, fmt.Errorf("reading %s: %w", path, err)
	}

	list, err := Parse(string(data))
	if err != nil {
		return List{}, fmt.Errorf("%s: %w", path, err)
	}

	return list, nil
}

// Save writes the list to path whole: beside the file, then renamed into its
// place, so a reader finds the old file or the new one.
func Save(path string, list List) error {
	staged, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	name := staged.Name()

	_, err = staged.WriteString(list.String())
	if closeErr := staged.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Chmod(name, filePermissions)
	}

	if err == nil {
		err = os.Rename(name, path)
	}

	if err != nil {
		_ = os.Remove(name)

		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
