#!/bin/sh

set -e # Exit early if any commands fail

(
  cd "$(dirname "$0")" 
  go build -o /tmp/build-bittorrent-go app/*.go
)


exec /tmp/build-bittorrent-go "$@"
