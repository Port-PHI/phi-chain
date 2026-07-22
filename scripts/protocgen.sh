#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Generate Go code from the Phi chain proto files (gogoproto + grpc-gateway).
# Requires: buf, protoc-gen-gocosmos, protoc-gen-grpc-gateway in PATH (e.g. $(go env GOPATH)/bin).
# Fully offline: proto dependencies are vendored locally under proto/ (no BSR).
set -eo pipefail

cd "$(dirname "$0")/.."
CHAIN_ROOT="$(pwd)"

echo ">> Generating gogo proto code"
cd proto

# Only files that declare go_package (all Phi protos) are generated.
proto_dirs=$(find ./phi -name '*.proto' -print0 | xargs -0 -n1 dirname | sort | uniq)
for dir in $proto_dirs; do
  for file in $(find "${dir}" -maxdepth 1 -name '*.proto'); do
    if grep -q "option go_package" "$file"; then
      buf generate --template buf.gen.gogo.yaml "$file"
    fi
  done
done

cd "$CHAIN_ROOT"

# Generated code lands under ./github.com/Port-PHI/phi-chain/x/<module>/types (the go_package path);
# move it to its correct location.
if [ -d "github.com" ]; then
  cp -r github.com/Port-PHI/phi-chain/* ./
  rm -rf github.com
fi

echo ">> Done."
echo ">> If you add a new proto message that introduces a new import, run once online: 'go mod tidy && go mod vendor'."
