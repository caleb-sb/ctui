//go:build bundle_tuicr

package main

import _ "embed"

//go:embed third_party/tuicr/darwin-arm64/tuicr.gz
var tuicrGz []byte

func init() { bundledTuicr = tuicrGz }
