package startupinfra

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func (d Detector) queryExecutable(ctx context.Context, command, override string) (string, bool, error) {
	lookup := d.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	executable := command
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", false, nil
		}
		executable = override
	}
	path, err := lookup(executable)
	if ctx.Err() != nil {
		return "", false, errors.Join(ctx.Err(), err)
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	path, err = filepath.Abs(path)
	return path, err == nil, err
}
