set shell := ["bash", "-euo", "pipefail", "-c"]

default: package

shim:
    gcc -shared -fPIC -O2 -Wall -Wextra -pthread -o payload/license.so license_source/license.c

test: shim
    GOCACHE=/tmp/d2r-offline-launcher-go-cache go test ./...

build: shim
    mkdir -p release
    GOCACHE=/tmp/d2r-offline-launcher-go-cache go build -buildvcs=false -o release/D2R-Offline-Launcher-Linux .

package: test build
    sha256sum release/D2R-Offline-Launcher-Linux

clean:
    rm -rf release
