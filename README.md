[![Build Status](https://tigera.semaphoreci.com/badges/go-build/branches/master.svg?style=shields)](https://tigera.semaphoreci.com/projects/go-build)

# Calico go-build

Calico go-build image holds Go and Clang toolchains and necessary utilities for building various [Calico](https://projectcalico.org) projects.

## Building the image

To build the image:

```bash
make image
```

The above will build for whatever architecture you are running on. To force a different architecture:

```bash
ARCH=<somearch> make image
```

## Tagging

The image tag is generated from Go, Clang, and Kubernetes versions. A new branch will be created automatically when one of the versions is changed. Semaphore jobs will run on the new branch and push images to Docker Hub. In addition, the given architecture is appended to the end. A multi-arch image manifest is generated from all supported architectures.

## Cross building using go-build

Any supported platform can be built natively from its own platform, i.e.g `amd64` from `amd64`, `arm64` from `arm64` and `ppc64le` from `ppc64le`. In addition, `ppc64le` and `arm64` are supported for cross-building from `amd64` only. We do not (yet) support cross-building from `arm64` and `ppc64le`.

The cross-build itself will function normally on any platform, since golang supports cross-compiling using `GOARCH=<target> go build`.

```bash
docker run -e GOARCH=<somearch> calico/go-build:latest-amd64 sh -c 'go build hello.go || ./hello'
```

The above will output a binary `hello` built for the architecture `<somearch>`.

## Cross-running Binaries binfmt

The Linux kernel has the ability to run binaries built for one arch on another, e.g. `arm64` binaries on an `amd64` architecture. Support requires two things:

1. Registering an interpreter that can run the binary for the other architecture along with configuration information on how to identify which binaries are for which platform and which emulator will handle them.
2. Making the interpreter binary available.

The interpreter must exist in one of two places:

* The container where you are running the other-architecture binary.
* The container where you run registration, if you pass the correct flag during registration. This is supported **only** from Linux kernel version 4.8+.

For example, if you registered the `s390x` emulator at `/usr/bin/qemu-s390x-static`, and then wanted to run `docker run -it --rm s390x/alpine sh` on an `amd64`, it wouldn't work in the first method, because the new container doesn't have an emulator in it. However, if you followed the second method, it would work, since the kernel already found and loaded the emulator. This works **even if you delete the registration container.**

To register emulators, we run:

```bash
make register
```

After the above registration, your system can handle other-architecture binaries. The above registration uses the first method, since _all_ kernels that support `binfmt` support this method, while only kernels from version 4.8+ support the latter. While docker-for-mac and docker-for-windows both use supporting kernels, almost every CI-as-a-service does not.

## Running a Binary

To _run_ a binary from a different architecture, you need to use `binfmt` and `qemu` static.

Register `qemu-*-static` for all supported processors except the current one using the following command:

```bash
# Replace qemu-vx.y.z with the latest tag from https://hub.docker.com/r/calico/binfmt/tags
docker run --privileged --rm calico/binfmt:qemu-vx.y.z --install all
```

If a cross built binary is executed in the go-build container qemu-static will automatically be used.
