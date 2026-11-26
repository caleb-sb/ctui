package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Release builds (go build -tags bundle_tuicr, after scripts/fetch-tuicr.sh) carry tuicr inside
// ctui, so it needn't be installed. Other builds use tuicr from PATH.

const tuicrVersion = "0.30.0"

// bundledTuicr is the gzipped tuicr binary for this platform; nil if this build doesn't carry one.
var bundledTuicr []byte

//go:embed third_party/tuicr/LICENSE
var tuicrLicense string

// tuicrPath returns the tuicr to run, and whether it's the bundled one.
// tuicr_cmd in the config wins; then the bundled copy; then tuicr on PATH.
func tuicrPath(cfg Config) (string, bool, error) {
	if cfg.TuicrCmd != "" {
		p, err := exec.LookPath(cfg.TuicrCmd)
		if err != nil {
			return "", false, fmt.Errorf("tuicr_cmd %q not found", cfg.TuicrCmd)
		}
		return p, false, nil
	}
	if bundledTuicr != nil {
		p, err := unpackTuicr()
		return p, true, err
	}
	p, err := exec.LookPath("tuicr")
	if err != nil {
		return "", false, fmt.Errorf("tuicr not found: install it or set tuicr_cmd")
	}
	return p, false, nil
}

// unpackTuicr writes the bundled tuicr to the cache dir the first time it's needed.
func unpackTuicr() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(home(), ".cache")
	}
	dir := filepath.Join(base, "ctui")
	path := filepath.Join(dir, "tuicr-"+tuicrVersion)
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o100 != 0 {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	zr, err := gzip.NewReader(bytes.NewReader(bundledTuicr))
	if err != nil {
		return "", fmt.Errorf("bundled tuicr: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tuicr-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, zr); err != nil {
		tmp.Close()
		return "", fmt.Errorf("bundled tuicr: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
