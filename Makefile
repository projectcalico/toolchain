include Makefile.common

.PHONY: build
build:
	$(MAKE) -C cmd build

.PHONY: image
image:
	$(MAKE) -C images image

.PHONY: clean
clean:
	$(MAKE) -C cmd clean
	$(MAKE) -C images clean
