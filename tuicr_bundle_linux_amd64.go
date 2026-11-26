//go:build bundle_tuicr

package main

import _ "embed"

//go:embed third_party/tuicr/linux-amd64/tuicr.gz
var tuicrGz []byte

func init() { bundledTuicr = tuicrGz }
