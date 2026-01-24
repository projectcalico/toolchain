include ../lib.Makefile
include ../Makefile.common

CALICO_BASE ?= base
CALICO_BINFMT ?= binfmt
CALICO_GO_BUILD ?= go-build
CALICO_RUST_BUILD ?= rust-build

.PHONY: build
build:
	$(MAKE) -C ../cmd build
	mkdir -p calico-binfmt/bin calico-go-build/bin/
	cp ../cmd/bin/binfmt-$(ARCH) calico-binfmt/bin/binfmt-$(ARCH)
	cp ../cmd/bin/semvalidator-$(ARCH) calico-go-build/bin/semvalidator-$(ARCH)

.PHONY: image
image: calico-base-image calico-binfmt-image calico-go-build-image calico-rust-build-image

.PHONY: image-all
image-all: calico-base-image-all calico-binfmt-image calico-go-build-image-all calico-rust-build-image-all

################################################################################
# Base image for all Calico components.
################################################################################

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

UBI_VERSIONS ?= ubi8 ubi9 ubi10

.PHONY: calico-base-image
calico-base-image: $(addprefix calico-base-image-,$(UBI_VERSIONS))

.PHONY: calico-base-image-%
calico-base-image-%: register
	$(eval DOCKERFILE := $(if $(filter ubi8,$*),Dockerfile.ubi8,Dockerfile))
	$(DOCKER_BUILD) --build-arg LDSONAME=$(LDSONAME) --build-arg=UBI_VERSION=$* -t $(CALICO_BASE):$*-latest-$(ARCH) -f calico-base/$(DOCKERFILE) calico-base/
	$(MAKE) BUILD_IMAGES=$(CALICO_BASE) retag-build-images-with-registries VALIDARCHES=$(ARCH) LATEST_IMAGE_TAG=$*-latest IMAGETAG=$*-latest

.PHONY: calico-base-image-all
calico-base-image-all: $(addprefix sub-calico-base-image-,$(VALIDARCHES))
sub-calico-base-image-%:
	$(MAKE) calico-base-image ARCH=$*

.PHONY: calico-base-cd
calico-base-cd: calico-base-image-all var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(foreach version,$(UBI_VERSIONS),$(MAKE) BUILD_IMAGES=$(CALICO_BASE) retag-build-images-with-registries push-images-to-registries push-manifests LATEST_IMAGE_TAG=$(version)-latest IMAGETAG=$(version)-$(BRANCH_NAME) EXCLUDEARCH="$(EXCLUDEARCH)";)

################################################################################
# Calico builder contains Go/Clang compilers and necessary utilities for UT/FVs.
################################################################################

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

################################################################################
# Calico binfmt image provides prebuilt qemu binaries for multi-arch builds.
################################################################################

# qemu-user-static package version from Fedora
QEMU_VERSION ?= $(shell yq -r '.qemu.version' calico-binfmt/versions.yaml)
CALICO_BINFMT_IMAGETAG ?= qemu-v$(QEMU_VERSION)

# We only build for amd64 architecture because our CI/CD runners are x86_64.
.PHONY: calico-binfmt-image
calico-binfmt-image: build
	$(DOCKER_BUILD) --build-arg QEMU_VERSION=$(QEMU_VERSION) -t $(CALICO_BINFMT):$(CALICO_BINFMT_IMAGETAG)-amd64 -f calico-binfmt/Dockerfile calico-binfmt/
	$(MAKE) BUILD_IMAGES=$(CALICO_BINFMT) retag-build-images-with-registries VALIDARCHES=amd64 LATEST_IMAGE_TAG=$(CALICO_BINFMT_IMAGETAG) IMAGETAG=$(CALICO_BINFMT_IMAGETAG)

.PHONY: calico-binfmt-cd
calico-binfmt-cd: calico-binfmt-image var-require-one-of-CONFIRM-DRYRUN
	$(MAKE) BUILD_IMAGES=$(CALICO_BINFMT) retag-build-images-with-registries push-images-to-registries IMAGETAG=$(CALICO_BINFMT_IMAGETAG) LATEST_IMAGE_TAG=$(CALICO_BINFMT_IMAGETAG) VALIDARCHES=amd64 EXCLUDEARCH="$(EXCLUDEARCH)"

################################################################################
# Calico rust-build image with Rust toolchain.
################################################################################

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

################################################################################
# Clean up
################################################################################

.PHONY: clean
clean:
	rm -fr calico-binfmt/bin calico-go-build/bin 
	-docker image rm -f $$(docker images $(CALICO_BASE) -a -q)
	-docker image rm -f $$(docker images $(CALICO_BINFMT) -a -q)
	-docker image rm -f $$(docker images $(CALICO_GO_BUILD) -a -q)
	-docker image rm -f $$(docker images $(CALICO_RUST_BUILD) -a -q)
