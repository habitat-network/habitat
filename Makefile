
MIN_MAKE_VERSION	=	4.0.0

ifneq ($(MIN_MAKE_VERSION),$(firstword $(sort $(MAKE_VERSION) $(MIN_MAKE_VERSION))))
$(error you must have a version of GNU make newer than v$(MIN_MAKE_VERSION) installed)
endif

# If TOPDIR isn't already defined, let's go with a default
ifeq ($(origin TOPDIR), undefined)
TOPDIR			:=	$(realpath $(patsubst %/,%, $(dir $(lastword $(MAKEFILE_LIST)))))
endif

# Set up critical Habitat environment variables
DEV_HABITAT_PATH = $(TOPDIR)/.habitat
DEV_HABITAT_CONFIG_PATH = $(DEV_HABITAT_PATH)/habitat.yml
DEV_HABITAT_ENV_PATH = $(TOPDIR)/dev.env
PERMS_DIR = $(DEV_HABITAT_PATH)/permissions

GOBIN ?= $$(go env GOPATH)/bin

build: $(TOPDIR)/bin/amd64-linux/habitat $(TOPDIR)/bin/amd64-darwin/habitat

# ===============================================================================

archive: $(TOPDIR)/bin/amd64-linux/habitat-amd64-linux.tar.gz $(TOPDIR)/bin/amd64-darwin/habitat-amd64-darwin.tar.gz

test::
	go test ./... -timeout 1s

clean::
	rm -rf $(TOPDIR)/bin
	rm -rf $(TOPDIR)/frontend/dist


test-coverage:
	go test ./... -coverprofile=coverage.out -coverpkg=./... -timeout 1s
	${GOBIN}/go-test-coverage --config=./.testcoverage.yml || true
	go tool cover -html=coverage.out

lint::
# To install: https://golangci-lint.run/usage/install/#local-installation
	CGO_ENABLED=0 golangci-lint run ./...

$(DEV_HABITAT_PATH):
	mkdir -p $(DEV_HABITAT_PATH)

$(DEV_HABITAT_APP_PATH):
	mkdir -p $(DEV_HABITAT_APP_PATH)

$(PERMS_DIR): $(DEV_HABITAT_PATH)
	mkdir -p $(PERMS_DIR)


# ===================== Production binary build rules =====================

$(TOPDIR)/bin: $(TOPDIR)
	mkdir -p $(TOPDIR)/bin


# Linux AMD64 Builds
$(TOPDIR)/bin/amd64-linux/habitat: $(TOPDIR)/bin
	GOARCH=amd64 GOOS=linux go build -o $(TOPDIR)/bin/amd64-linux/habitat $(TOPDIR)/cmd/privi

$(TOPDIR)/bin/amd64-linux/habitat-amd64-linux.tar.gz: $(TOPDIR)/bin/amd64-linux/habitat
	tar -czf $(TOPDIR)/bin/amd64-linux/habitat-amd64-linux.tar.gz -C $(TOPDIR)/bin/amd64-linux habitat

# Darwin AMD64 Builds
$(TOPDIR)/bin/amd64-darwin/habitat: $(TOPDIR)/bin
	GOARCH=amd64 GOOS=darwin go build -o $(TOPDIR)/bin/amd64-darwin/habitat $(TOPDIR)/cmd/privi

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

# ===================== Privi build rules =====================

privi-dev: 
	foreman start -f privi.Procfile -e $(DEV_HABITAT_ENV_PATH)

lexgen:
	go run cmd/lexgen/main.go
