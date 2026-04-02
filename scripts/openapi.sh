#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

case "${1:-}" in
  validate)
    docker run --rm -v "$PWD":/work -w /work redocly/cli:latest lint docs/openapi.yaml
    ;;
  build)
    docker run --rm -v "$PWD":/work -w /work redocly/cli:latest bundle docs/openapi.yaml -o docs/openapi.json
    ;;
  postman)
    docker run --rm -v "$PWD":/work -w /work postman/openapi-to-postman -s docs/openapi.yaml -o docs/postman_collection.json -p
    ;;
  sdk)
    docker run --rm -v "$PWD":/local openapitools/openapi-generator-cli generate \
      -i /local/docs/openapi.yaml -g typescript-axios -o /local/clients/typescript
    docker run --rm -v "$PWD":/local openapitools/openapi-generator-cli generate \
      -i /local/docs/openapi.yaml -g python -o /local/clients/python
    docker run --rm -v "$PWD":/local openapitools/openapi-generator-cli generate \
      -i /local/docs/openapi.yaml -g go -o /local/clients/go
    ;;
  *)
    echo "Usage: $0 {validate|build|postman|sdk}"
    exit 2
    ;;
esac

