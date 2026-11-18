DIR := ${CURDIR}
GO_IMAGE := golang:1.22.0-alpine
DOCKER := podman

.PHONY: test
test:
	${DOCKER} run -it --rm \
		-w /app -v ${DIR}:/app \
		${GO_IMAGE} \
		go test ./...


.PHONY: lint
lint:
	${DOCKER} run -it --rm \
		-e CGO_ENABLED=0 \
		-w /app -v ${DIR}:/app \
		golangci/golangci-lint:v2.12.2 \
		golangci-lint run ./...
