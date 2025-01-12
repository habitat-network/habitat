
MIN_MAKE_VERSION	=	4.0.0

ifneq ($(MIN_MAKE_VERSION),$(firstword $(sort $(MAKE_VERSION) $(MIN_MAKE_VERSION))))
$(error you must have a version of GNU make newer than v$(MIN_MAKE_VERSION) installed)
endif

# If TOPDIR isn't already defined, let's go with a default
ifeq ($(origin TOPDIR), undefined)
TOPDIR			:=	$(realpath $(patsubst %/,%, $(dir $(lastword $(MAKEFILE_LIST)))))
endif

# Directories inside the dev docker container
DOCKER_WORKDIR = /go/src/github.com/eagraf/habitat-new

# Set up critical Habitat environment variables
DEV_HABITAT_PATH = $(TOPDIR)/.habitat
DEV_HABITAT_APP_PATH = $(TOPDIR)/.habitat/apps
DEV_HABITAT_CONFIG_PATH = $(TOPDIR)/.habitat/habitat.yml
DEV_HABITAT_ENV_PATH = $(TOPDIR)/dev.env
CERT_DIR = $(DEV_HABITAT_PATH)/certificates
PDS_DIR = $(DEV_HABITAT_APP_PATH)/pds
PDS_BLOB_DIR = $(PDS_DIR)/blocks

GOBIN ?= $$(go env GOPATH)/bin

build: $(TOPDIR)/bin/amd64-linux/habitat $(TOPDIR)/bin/amd64-darwin/habitat

archive: $(TOPDIR)/bin/amd64-linux/habitat-amd64-linux.tar.gz $(TOPDIR)/bin/amd64-darwin/habitat-amd64-darwin.tar.gz

test::
	go test ./... -timeout 1s

clean::
	rm -rf $(TOPDIR)/bin
	rm -rf $(TOPDIR)/frontend_server/build
	rm -rf $(TOPDIR)/frontend/out
	rm -rf $(TOPDIR)/frontend/.next


test-coverage:
	go test ./... -coverprofile=coverage.out -coverpkg=./... -timeout 1s
	${GOBIN}/go-test-coverage --config=./.testcoverage.yml || true
	go tool cover -html=coverage.out

lint::
# To install: https://golangci-lint.run/usage/install/#local-installation
	CGO_ENABLED=0 golangci-lint run ./...

install:: $(DEV_HABITAT_PATH)/habitat.yml $(CERT_DIR)/dev_node_cert.pem $(CERT_DIR)/dev_root_user_cert.pem $(PDS_BLOB_DIR)
	go install ./cmd/node

docker-build:
	docker build -t habitat_node -f ./build/node.dev.Dockerfile .

run-dev: $(PDS_BLOB_DIR) $(CERT_DIR)/dev_root_user_cert.pem $(CERT_DIR)/dev_node_cert.pem $(DEV_HABITAT_CONFIG_PATH) $(DEV_HABITAT_ENV_PATH)
	TOPDIR=$(TOPDIR) \
	DOCKER_WORKDIR=$(DOCKER_WORKDIR) \
	DEV_HABITAT_PATH=$(DEV_HABITAT_PATH) \
	DEV_HABITAT_APP_PATH=$(DEV_HABITAT_APP_PATH) \
	docker-compose -f ./build/compose.yml up

clear-volumes:
	docker container rm -f habitat_node || true
	docker volume prune -f
	rm -rf $(DEV_HABITAT_PATH)/hdb

run-dev-fresh: clear-volumes run-dev

$(DEV_HABITAT_PATH):
	mkdir -p $(DEV_HABITAT_PATH)

$(DEV_HABITAT_APP_PATH):
	mkdir -p $(DEV_HABITAT_APP_PATH)

$(DEV_HABITAT_CONFIG_PATH): $(DEV_HABITAT_PATH)
	touch $(DEV_HABITAT_CONFIG_PATH)

$(DEV_HABITAT_ENV_PATH):
	touch $(DEV_HABITAT_ENV_PATH)

$(CERT_DIR): $(DEV_HABITAT_PATH)
	mkdir -p $(CERT_DIR)

$(DEV_HABITAT_PATH)/habitat.yml: $(DEV_HABITAT_PATH)
	cp $(TOPDIR)/config/habitat.dev.yml $(DEV_HABITAT_PATH)/habitat.yml

$(PDS_BLOB_DIR):
	mkdir -p $(PDS_BLOB_DIR)

$(CERT_DIR)/dev_node_cert.pem: $(CERT_DIR)
	@echo "Generating dev node certificate"
	openssl req -newkey rsa:2048 \
		-new -nodes -x509 \
		-out $(CERT_DIR)/dev_node_cert.pem \
		-keyout $(CERT_DIR)/dev_node_key.pem \
		-subj "/C=US/ST=California/L=Mountain View/O=Habitat/CN=dev_node"

$(CERT_DIR)/dev_root_user_cert.pem: $(CERT_DIR)
	@echo "Generating dev root user certificate"
	openssl req -newkey rsa:2048 \
		-new -nodes -x509 \
		-out $(CERT_DIR)/dev_root_user_cert.pem \
		-keyout $(CERT_DIR)/dev_root_user_key.pem \
		-subj "/C=US/ST=California/L=Mountain View/O=Habitat/CN=root"


# ===================== Production binary build rules =====================

$(TOPDIR)/bin: $(TOPDIR)
	mkdir -p $(TOPDIR)/bin

# Linux AMD64 Builds
$(TOPDIR)/bin/amd64-linux/habitat: $(TOPDIR)/bin frontend_server/build
	GOARCH=amd64 GOOS=linux go build -o $(TOPDIR)/bin/amd64-linux/habitat $(TOPDIR)/cmd/node/main.go

$(TOPDIR)/bin/amd64-linux/habitat-amd64-linux.tar.gz: $(TOPDIR)/bin/amd64-linux/habitat
	tar -czf $(TOPDIR)/bin/amd64-linux/habitat-amd64-linux.tar.gz -C $(TOPDIR)/bin/amd64-linux habitat

# Darwin AMD64 Builds
$(TOPDIR)/bin/amd64-darwin/habitat: $(TOPDIR)/bin frontend_server/build
	GOARCH=amd64 GOOS=darwin go build -o $(TOPDIR)/bin/amd64-darwin/habitat $(TOPDIR)/cmd/node/main.go

$(TOPDIR)/bin/amd64-darwin/habitat-amd64-darwin.tar.gz: $(TOPDIR)/bin/amd64-darwin/habitat
	tar -czf $(TOPDIR)/bin/amd64-darwin/habitat-amd64-darwin.tar.gz -C $(TOPDIR)/bin/amd64-darwin habitat


# ===================== Frontend build rules =====================

clean:: clean-frontend-types

clean-frontend-types:
	rm -rf $(TOPDIR)/frontend/types/*.ts
# Generate the frontend types
frontend/types/api.ts:
	tygo --config $(TOPDIR)/config/tygo.yml generate

frontend-types: frontend/types/api.ts
PHONY += frontend-types

# Embed the frontend in the binary
frontend_server/build: frontend/types/api.ts
	cd $(TOPDIR)/frontend && pnpm install && pnpm run build
	mkdir -p $(TOPDIR)/frontend_server/build
	cp -r $(TOPDIR)/frontend/out/* $(TOPDIR)/frontend_server/build
