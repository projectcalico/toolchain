include ../lib.Makefile
include ../Makefile.common

CALICO_BASE ?= base
CALICO_BINFMT ?= binfmt
CALICO_GO_BUILD ?= go-build
CALICO_RUST_BUILD ?= rust-build
CALICO_TINYGO ?= tinygo
CALICO_SCRATCH_UTILS ?= scratch-utils
CALICO_SIGNING_TOOLS ?= signing-tools

# Optional S3 build cache, enabled only when all of the S3_CACHE_* connection
# variables below are set (in CI they come from the
# projectcalico-toolchain-s3-secrets secret); otherwise images build as normal on
# the default builder. The docker driver cannot export to s3, so cached builds run
# on a dedicated docker-container builder, created on demand and never made the
# default. It uses the host network so a local endpoint (e.g. MinIO on
# localhost:9000) also works. Each target gets its own cache manifest, and a
# failed cache export only warns rather than failing the build.
#
# The keys reach buildx as exported AWS_* environment variables rather than
# cache attributes, so their values never appear in the echoed command or log.
BUILDX_CACHE_BUILDER ?= calico-cache-builder
S3_CACHE_REGION ?=
S3_CACHE_ENDPOINT ?=
S3_CACHE_BUCKET ?=
S3_CACHE_ACCESS_KEY_ID ?=
S3_CACHE_SECRET_ACCESS_KEY ?=
S3_CACHE_USE_PATH_STYLE ?= true

ifneq ($(and $(S3_CACHE_REGION),$(S3_CACHE_ENDPOINT),$(S3_CACHE_BUCKET),$(S3_CACHE_ACCESS_KEY_ID),$(S3_CACHE_SECRET_ACCESS_KEY)),)
export AWS_ACCESS_KEY_ID = $(S3_CACHE_ACCESS_KEY_ID)
export AWS_SECRET_ACCESS_KEY = $(S3_CACHE_SECRET_ACCESS_KEY)
S3_CACHE = type=s3,region=$(S3_CACHE_REGION),bucket=$(S3_CACHE_BUCKET),endpoint_url=$(S3_CACHE_ENDPOINT),use_path_style=$(S3_CACHE_USE_PATH_STYLE),name=$@-$(ARCH)
DOCKER_BUILD += --builder=$(BUILDX_CACHE_BUILDER) --cache-to=$(S3_CACHE),mode=max,ignore-error=true --cache-from=$(S3_CACHE)
CACHE_BUILDER = buildx-cache-builder
endif

.PHONY: buildx-cache-builder
buildx-cache-builder:
	docker buildx inspect $(BUILDX_CACHE_BUILDER) >/dev/null 2>&1 || \
		docker buildx create --name $(BUILDX_CACHE_BUILDER) --driver docker-container --driver-opt network=host

.PHONY: build
build:
	$(MAKE) -C ../cmd build
	mkdir -p calico-binfmt/bin calico-go-build/bin/
	cp ../cmd/bin/binfmt-$(ARCH) calico-binfmt/bin/binfmt-$(ARCH)
	cp ../cmd/bin/semvalidator-$(ARCH) calico-go-build/bin/semvalidator-$(ARCH)

# Separate so the go-build and binfmt images do not compile the GCP SDK to build
# a binary they never contain.
.PHONY: scratch-utils-build
scratch-utils-build:
	$(MAKE) -C ../cmd scratch-utils-build
	mkdir -p calico-scratch-utils/bin/
	cp ../cmd/bin/scratch-utils-$(ARCH) calico-scratch-utils/bin/scratch-utils-$(ARCH)

.PHONY: image
image: calico-base-image calico-binfmt-image calico-go-build-image calico-rust-build-image calico-tinygo-image calico-scratch-utils-image calico-signing-tools-image

.PHONY: image-all
image-all: calico-base-image-all calico-binfmt-image calico-go-build-image-all calico-rust-build-image-all calico-tinygo-image-all calico-scratch-utils-image calico-signing-tools-image

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

UBI_VERSIONS ?= ubi9 ubi10

.PHONY: calico-base-image
calico-base-image: $(addprefix calico-base-image-,$(UBI_VERSIONS))

.PHONY: calico-base-image-%
calico-base-image-%: register | $(CACHE_BUILDER)
	$(DOCKER_BUILD) --build-arg LDSONAME=$(LDSONAME) --build-arg=UBI_VERSION=$* -t $(CALICO_BASE):$*-latest-$(ARCH) -f calico-base/Dockerfile calico-base/
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
calico-go-build-image: register build | $(CACHE_BUILDER)
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
calico-binfmt-image: build | $(CACHE_BUILDER)
	$(DOCKER_BUILD) --build-arg QEMU_VERSION=$(QEMU_VERSION) -t $(CALICO_BINFMT):$(CALICO_BINFMT_IMAGETAG)-amd64 -f calico-binfmt/Dockerfile calico-binfmt/
	$(MAKE) BUILD_IMAGES=$(CALICO_BINFMT) retag-build-images-with-registries VALIDARCHES=amd64 LATEST_IMAGE_TAG=$(CALICO_BINFMT_IMAGETAG) IMAGETAG=$(CALICO_BINFMT_IMAGETAG)

.PHONY: calico-binfmt-cd
calico-binfmt-cd: calico-binfmt-image var-require-one-of-CONFIRM-DRYRUN
	$(MAKE) BUILD_IMAGES=$(CALICO_BINFMT) retag-build-images-with-registries push-images-to-registries IMAGETAG=$(CALICO_BINFMT_IMAGETAG) LATEST_IMAGE_TAG=$(CALICO_BINFMT_IMAGETAG) VALIDARCHES=amd64 EXCLUDEARCH="$(EXCLUDEARCH)"

################################################################################
# Calico scratch-utils image: the CI VM lifecycle binary.
################################################################################

CALICO_SCRATCH_UTILS_IMAGETAG ?= $(shell yq -r '.scratch-utils.version' calico-scratch-utils/versions.yaml)

# amd64 only, like binfmt: the runners and the GCE VMs are x86_64.
.PHONY: calico-scratch-utils-image
calico-scratch-utils-image: scratch-utils-build | $(CACHE_BUILDER)
	$(DOCKER_BUILD) -t $(CALICO_SCRATCH_UTILS):$(CALICO_SCRATCH_UTILS_IMAGETAG)-amd64 -f calico-scratch-utils/Dockerfile calico-scratch-utils/
	$(MAKE) BUILD_IMAGES=$(CALICO_SCRATCH_UTILS) retag-build-images-with-registries VALIDARCHES=amd64 LATEST_IMAGE_TAG=$(CALICO_SCRATCH_UTILS_IMAGETAG) IMAGETAG=$(CALICO_SCRATCH_UTILS_IMAGETAG)

# IMAGETAG is the pushed tag, LATEST_IMAGE_TAG the local one built above. A tag
# build pushes the immutable :vX.Y (BRANCH_NAME is the stripped git tag); master
# pushes the moving :master and :latest.
.PHONY: calico-scratch-utils-cd
calico-scratch-utils-cd: calico-scratch-utils-image var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_SCRATCH_UTILS) retag-build-images-with-registries push-images-to-registries IMAGETAG=$(BRANCH_NAME) LATEST_IMAGE_TAG=$(CALICO_SCRATCH_UTILS_IMAGETAG) VALIDARCHES=amd64 EXCLUDEARCH="$(EXCLUDEARCH)"
ifeq ($(BRANCH_NAME),master)
	$(MAKE) BUILD_IMAGES=$(CALICO_SCRATCH_UTILS) retag-build-images-with-registries push-images-to-registries IMAGETAG=latest LATEST_IMAGE_TAG=$(CALICO_SCRATCH_UTILS_IMAGETAG) VALIDARCHES=amd64 EXCLUDEARCH="$(EXCLUDEARCH)"
endif

################################################################################
# Calico signing-tools image: pinned cosign and syft for release signing.
################################################################################

SIGNING_TOOLS_VERSIONS := calico-signing-tools/versions.yaml
CALICO_SIGNING_TOOLS_IMAGETAG ?= $(shell yq -r '.signing-tools.version' $(SIGNING_TOOLS_VERSIONS))

# amd64 only: the release runners are x86_64, and versions.yaml pins amd64 checksums.
.PHONY: calico-signing-tools-image
calico-signing-tools-image: override ARCH=amd64
calico-signing-tools-image: | $(CACHE_BUILDER)
	$(DOCKER_BUILD) \
		--build-arg COSIGN_VERSION=$(shell yq -r '.cosign.version' $(SIGNING_TOOLS_VERSIONS)) \
		--build-arg COSIGN_SHA256=$(shell yq -r '.cosign.checksum.sha256.amd64' $(SIGNING_TOOLS_VERSIONS)) \
		--build-arg SYFT_VERSION=$(shell yq -r '.syft.version' $(SIGNING_TOOLS_VERSIONS)) \
		--build-arg SYFT_SHA256=$(shell yq -r '.syft.checksum.sha256.amd64' $(SIGNING_TOOLS_VERSIONS)) \
		-t $(CALICO_SIGNING_TOOLS):$(CALICO_SIGNING_TOOLS_IMAGETAG)-amd64 -f calico-signing-tools/Dockerfile calico-signing-tools/
	$(MAKE) BUILD_IMAGES=$(CALICO_SIGNING_TOOLS) retag-build-images-with-registries VALIDARCHES=amd64 LATEST_IMAGE_TAG=$(CALICO_SIGNING_TOOLS_IMAGETAG) IMAGETAG=$(CALICO_SIGNING_TOOLS_IMAGETAG)

.PHONY: calico-signing-tools-cd
calico-signing-tools-cd: calico-signing-tools-image var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_SIGNING_TOOLS) retag-build-images-with-registries push-images-to-registries IMAGETAG=$(BRANCH_NAME) LATEST_IMAGE_TAG=$(CALICO_SIGNING_TOOLS_IMAGETAG) VALIDARCHES=amd64 EXCLUDEARCH="$(EXCLUDEARCH)"

################################################################################
# Calico rust-build image with Rust toolchain.
################################################################################

CALICO_RUST_BUILD_IMAGETAG ?= latest

.PHONY: calico-rust-build-image
calico-rust-build-image: register | $(CACHE_BUILDER)
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
# Calico tinygo image with TinyGo + Go for WebAssembly builds (amd64 + arm64
# — TinyGo upstream does not publish Linux ppc64le/s390x tarballs).
################################################################################

CALICO_TINYGO_IMAGETAG ?= latest

.PHONY: calico-tinygo-image
calico-tinygo-image: register | $(CACHE_BUILDER)
	$(DOCKER_BUILD) -t $(CALICO_TINYGO):latest-$(ARCH) -f calico-tinygo/Dockerfile calico-tinygo/
	$(MAKE) BUILD_IMAGES=$(CALICO_TINYGO) retag-build-images-with-registries VALIDARCHES=$(ARCH) IMAGETAG=$(CALICO_TINYGO_IMAGETAG)

.PHONY: calico-tinygo-image-all
calico-tinygo-image-all: $(addprefix sub-calico-tinygo-image-,amd64 arm64)
sub-calico-tinygo-image-%:
	$(MAKE) calico-tinygo-image ARCH=$*

.PHONY: calico-tinygo-cd
calico-tinygo-cd: calico-tinygo-image var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_TINYGO) retag-build-images-with-registries push-images-to-registries IMAGETAG=$(BRANCH_NAME) VALIDARCHES=$(ARCH)

.PHONY: push-calico-tinygo-manifests
push-calico-tinygo-manifests: var-require-one-of-CONFIRM-DRYRUN var-require-all-BRANCH_NAME
	$(MAKE) BUILD_IMAGES=$(CALICO_TINYGO) push-manifests IMAGETAG=$(BRANCH_NAME) EXCLUDEARCH="ppc64le s390x"

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
	-docker image rm -f $$(docker images $(CALICO_TINYGO) -a -q)
