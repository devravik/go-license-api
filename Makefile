OPENAPI_YAML := docs/openapi.yaml
OPENAPI_JSON := docs/openapi.json
POSTMAN_COL := docs/postman_collection.json

.PHONY: openapi-validate
openapi-validate:
	docker run --rm -v $$PWD:/work -w /work redocly/cli:latest lint $(OPENAPI_YAML)

.PHONY: openapi-build
openapi-build:
	docker run --rm -v $$PWD:/work -w /work redocly/cli:latest bundle $(OPENAPI_YAML) -o $(OPENAPI_JSON)

.PHONY: openapi-postman
openapi-postman:
	docker run --rm -v $$PWD:/work -w /work postman/openapi-to-postman -s $(OPENAPI_YAML) -o $(POSTMAN_COL) -p

.PHONY: openapi-sdk
openapi-sdk:
	# TypeScript
	docker run --rm -v $$PWD:/local openapitools/openapi-generator-cli generate \
		-i /local/$(OPENAPI_YAML) -g typescript-axios -o /local/clients/typescript
	# Python
	docker run --rm -v $$PWD:/local openapitools/openapi-generator-cli generate \
		-i /local/$(OPENAPI_YAML) -g python -o /local/clients/python
	# Go (client)
	docker run --rm -v $$PWD:/local openapitools/openapi-generator-cli generate \
		-i /local/$(OPENAPI_YAML) -g go -o /local/clients/go

