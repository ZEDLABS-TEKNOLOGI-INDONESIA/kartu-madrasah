#!/bin/bash
set -e

export PATH=$PATH:/usr/local/go/bin

NAME="svgextract"
OUT="dist"

mkdir -p $OUT

targets=(
  "linux/amd64"
  "linux/arm64"
  "linux/arm"
)

for t in "${targets[@]}"; do
  OS="${t%%/*}"
  ARCH="${t##*/}"
  OUTFILE="$OUT/${NAME}_${OS}_${ARCH}"
  GOARM=""
  if [ "$ARCH" = "arm" ]; then
    GOARM=7
    OUTFILE="${OUTFILE}v7"
  fi
  echo "Building $OS/$ARCH..."
  GOOS=$OS GOARCH=$ARCH GOARM=$GOARM CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o $OUTFILE .
done

echo "Build selesai. Output di folder $OUT/"
ls -lh $OUT/
