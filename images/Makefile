include ../lib.Makefile
include ../Makefile.common

CALICO_BASE ?= base
CALICO_GO_BUILD ?= go-build
CALICO_RUST_BUILD ?= rust-build

.PHONY: image
image: calico-base-image calico-go-build-image calico-rust-build-image

.PHONY: image-all
image-all: calico-base-image-all calico-go-build-image-all calico-rust-build-image-all

# Base image for all calico components.

# ELF interpreter (dynamic loader) soname
LDSONAME=ld64.so.1
ifeq ($(ARCH),amd64)
	override LDSONAME=ld-linux-x86-64.so.2
else ifeq ($(ARCH),arm64)
	override LDSONAME=ld-linux-aarch64.so.1
else ifeq ($(ARCH),ppc64le)
	override LDSONAME=ld64.so.2
else ifeq ($(ARCH),s390)
	override LDSONAME=ld64.so.1
endif

UBI_VERSIONS ?= ubi9 ubi10

.PHONY: calico-base-image
calico-base-image: $(addprefix calico-base-image-,$(UBI_VERSIONS))

.PHONY: calico-base-image-%
calico-base-image-%: register
	$(DOCKER_BUILD) --build-arg LDSONAME=$(LDSONAME) --build-arg=UBI_VERSION=$* -t $(CALICO_BASE):$*-latest-$(ARCH) -f calico-base/Dockerfile calico-base/
	$(MAKE) BUILD_IMAGES=$(CALICO_BASE) retag-build-images-with-registries VALIDARCHES=$(ARCH) LATEST_IMAGE_TAG=$*-latest IMAGETAG=$*-latest

.PHONY: calico-base-image-all
calico-base-image-all: $(addprefix sub-calico-base-image-,$(VALIDARCHES))
sub-calico-base-image-%:
	$(MAKE) calico-base-image ARCH=$*

.PHONY: calico-base-cd
calico-base-cd: calico-base-image-all var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(foreach version,$(UBI_VERSIONS),$(MAKE) BUILD_IMAGES=$(CALICO_BASE) retag-build-images-with-registries push-images-to-registries push-manifests LATEST_IMAGE_TAG=$(version)-latest IMAGETAG=$(version)-$(BRANCH_NAME) EXCLUDEARCH="$(EXCLUDEARCH)";)

# Calico builder which contains Go/Clang compilers and necessary utilities for UT/FVs.
.PHONY: build
build:
	$(MAKE) -C ../cmd build
	mkdir -p calico-go-build/bin/
	cp ../cmd/bin/semvalidator-$(ARCH) calico-go-build/bin/semvalidator-$(ARCH)

CALICO_GO_BUILD_IMAGETAG ?= latest

.PHONY: calico-go-build-image
calico-go-build-image: register build
	$(DOCKER_BUILD) -t $(CALICO_GO_BUILD):latest-$(ARCH) -f calico-go-build/Dockerfile calico-go-build/
	$(MAKE) BUILD_IMAGES=$(CALICO_GO_BUILD) retag-build-images-with-registries VALIDARCHES=$(ARCH) IMAGETAG=$(CALICO_GO_BUILD_IMAGETAG)

.PHONY: calico-go-build-image-all
calico-go-build-image-all: $(addprefix sub-calico-go-build-image-,$(VALIDARCHES))
sub-calico-go-build-image-%:
	$(MAKE) calico-go-build-image ARCH=$*

.PHONY: calico-go-build-cd
calico-go-build-cd: calico-go-build-image var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_GO_BUILD) retag-build-images-with-registries push-images-to-registries IMAGETAG=$(BRANCH_NAME) EXCLUDEARCH="$(EXCLUDEARCH)"
ifeq ($(BRANCH_NAME),master)
	$(MAKE) BUILD_IMAGES=$(CALICO_GO_BUILD) retag-build-images-with-registries push-images-to-registries IMAGETAG=latest EXCLUDEARCH="$(EXCLUDEARCH)"
endif

.PHONY: push-calico-go-build-manifests
push-calico-go-build-manifests: var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_GO_BUILD) push-manifests IMAGETAG=$(BRANCH_NAME) EXCLUDEARCH="$(EXCLUDEARCH)"
ifeq ($(BRANCH_NAME),master)
	$(MAKE) BUILD_IMAGES=$(CALICO_GO_BUILD) push-manifests IMAGETAG=latest EXCLUDEARCH="$(EXCLUDEARCH)"
endif

# Calico rust-build image with Rust toolchain.
CALICO_RUST_BUILD_IMAGETAG ?= latest

.PHONY: calico-rust-build-image
calico-rust-build-image: register
	$(DOCKER_BUILD) -t $(CALICO_RUST_BUILD):latest-$(ARCH) -f calico-rust-build/Dockerfile calico-rust-build/
	$(MAKE) BUILD_IMAGES=$(CALICO_RUST_BUILD) retag-build-images-with-registries VALIDARCHES=$(ARCH) IMAGETAG=$(CALICO_RUST_BUILD_IMAGETAG)

.PHONY: calico-rust-build-image-all
calico-rust-build-image-all: $(addprefix sub-calico-rust-build-image-,$(VALIDARCHES))
sub-calico-rust-build-image-%:
	$(MAKE) calico-rust-build-image ARCH=$*

.PHONY: calico-rust-build-cd
calico-rust-build-cd: calico-rust-build-image var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_RUST_BUILD) retag-build-images-with-registries push-images-to-registries IMAGETAG=$(BRANCH_NAME) EXCLUDEARCH="$(EXCLUDEARCH)"

.PHONY: push-calico-rust-build-manifests
push-calico-rust-build-manifests: var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_RUST_BUILD) push-manifests IMAGETAG=$(BRANCH_NAME) EXCLUDEARCH="$(EXCLUDEARCH)"

.PHONY: clean
clean:
	rm -fr calico-go-build/bin
	-docker image rm -f $$(docker images $(CALICO_BASE) -a -q)
	-docker image rm -f $$(docker images $(CALICO_GO_BUILD) -a -q)
	-docker image rm -f $$(docker images $(CALICO_RUST_BUILD) -a -q)
