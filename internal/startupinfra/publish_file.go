package startupinfra

import (
	"errors"
	"io"
	"os"
)

// prepareConfigurationTemp returns a complete, synced, closed file. Failure
// removes only the temp created here; the destination has not been touched.
func prepareConfigurationTemp(directory string, data []byte) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("configuration directory is not a real directory")
	}
	// CreateTemp uses exclusive creation with mode 0600 before umask. No
	// subsequent Chmod is needed; platform permission interpretation is unchanged.
	temp, err := os.CreateTemp(directory, ".config-*")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	if err := writeAndCloseConfigurationTemp(temp, data); err != nil {
		return "", errors.Join(err, removeConfigurationTemp(name))
	}
	return name, nil
}

type writableConfigurationTemp interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// This owner closes exactly once on every path, retaining a write/sync failure
// alongside any close failure. A short write cannot reach publication.
func writeAndCloseConfigurationTemp(temp writableConfigurationTemp, data []byte) (err error) {
	defer func() { err = errors.Join(err, temp.Close()) }()
	written, err := temp.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return temp.Sync()
}

func removeConfigurationTemp(name string) error {
	err := os.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
