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

# The Semaphore pipeline is assembled from fragments so each image's blocks can
# be edited on their own. Run this after changing anything under
# .semaphore/semaphore.yml.d and commit the regenerated file.
.PHONY: gen-semaphore-yaml
gen-semaphore-yaml:
	hack/gen-semaphore-yaml.sh

# check-semaphore-yaml fails when the committed pipeline does not match its
# fragments, which is how an edit to the generated file gets caught.
.PHONY: check-semaphore-yaml
check-semaphore-yaml:
	hack/gen-semaphore-yaml.sh --check

.PHONY: update-go-build-pins
update-go-build-pins:
	SEMAPHORE_AUTO_PIN_UPDATE_PROJECT_IDS=$(SEMAPHORE_CALICO_PROJECT_ID) \
	SEMAPHORE_WORKFLOW_FILE=update-go-build-pins.yml \
	$(MAKE) semaphore-run-auto-pin-update-workflows
