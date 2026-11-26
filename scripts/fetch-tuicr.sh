#!/bin/sh
# Downloads the pinned tuicr release for each platform ctui is released for, checks it against
# the checksums below, and writes third_party/tuicr/<os>-<arch>/tuicr.gz for `go build -tags bundle_tuicr`.
set -eu
VERSION=0.30.0
cd "$(dirname "$0")/../third_party/tuicr"
fetch() { # goos-goarch rust-target sha256
	dir=$1 asset="tuicr-$VERSION-$2.tar.gz" want=$3
	tmp=$(mktemp -d)
	curl -fsSL -o "$tmp/$asset" "https://github.com/agavra/tuicr/releases/download/v$VERSION/$asset"
	got=$(sha256sum "$tmp/$asset" 2>/dev/null || shasum -a 256 "$tmp/$asset")
	got=${got%% *}
	if [ "$got" != "$want" ]; then
		echo "fetch-tuicr: $asset checksum $got, want $want" >&2
		rm -rf "$tmp"
		exit 1
	fi
	mkdir -p "$dir"
	tar xzf "$tmp/$asset" -C "$tmp" tuicr
	gzip -9 -c "$tmp/tuicr" > "$dir/tuicr.gz"
	rm -rf "$tmp"
	echo "$dir/tuicr.gz"
}
fetch darwin-arm64 aarch64-apple-darwin 082eb71cb9d6513f47d9571975f31a84318bc08893a306d110112ee6edabd276
fetch linux-amd64 x86_64-unknown-linux-musl bbd7a358c8450613177359bfbcc4bdbba68c98835311c5e424053a005610c9d2
