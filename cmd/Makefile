include ../lib.Makefile
include ../Makefile.common

# Git commit from https://github.com/tonistiigi/binfmt master branch
BINFMT_VERSION = $(shell yq -r '.tonistiigi-binfmt.version' ../images/calico-binfmt/versions.yaml)
# qemu-user-static package version from Fedora
QEMU_VERSION = $(shell yq -r '.qemu.version' ../images/calico-binfmt/versions.yaml)

.PHONY: build
build: bin/binfmt-$(ARCH) bin/semvalidator-$(ARCH) bin/scratch-utils-$(ARCH)

bin/binfmt-$(ARCH): binfmt/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) \
		go build -o bin/binfmt-$(ARCH) -v -buildvcs=false -ldflags "-X main.revision=$(BINFMT_VERSION) -X main.qemuVersion=$(QEMU_VERSION) -s -w" binfmt/*.go

bin/semvalidator-$(ARCH): semvalidator/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) \
		go build -o bin/semvalidator-$(ARCH) -v -buildvcs=false -ldflags "-s -w" semvalidator/main.go

# scratch-utils: the single CI VM binary (createvm/deletevm/secret/runonvm subcommands).
# Unlike the single-file binaries above, this one spans ../scratch-utils/, so list
# every .go file it compiles -- otherwise an incremental build ships a stale binary.
SCRATCH_UTILS_SRCS = $(shell find scratch-utils ../scratch-utils -name '*.go')

bin/scratch-utils-$(ARCH): $(SCRATCH_UTILS_SRCS)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) \
		go build -o bin/scratch-utils-$(ARCH) -v -buildvcs=false -ldflags "-s -w" ./scratch-utils

.PHONY: clean
clean:
	rm -fr bin/
