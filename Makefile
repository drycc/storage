SHORT_NAME := storage
IMAGE_PREFIX ?= drycc
# podman development environment variables
REPO_PATH := github.com/drycc/${SHORT_NAME}
DEV_ENV_IMAGE := ${DEV_REGISTRY}/drycc/go-dev
DEV_ENV_WORK_DIR := /workspace
DEV_ENV_PREFIX := podman run --env CGO_ENABLED=0 --rm -v ${CURDIR}:${DEV_ENV_WORK_DIR} -w ${DEV_ENV_WORK_DIR}
DEV_ENV_CMD := ${DEV_ENV_PREFIX} ${DEV_ENV_IMAGE}
DRYCC_REGISTRY ?= ${DEV_REGISTRY}

SHELL_SCRIPTS = $(wildcard _tests/*.sh)

include versioning.mk

all: podman-build podman-push

test: test-style podman-build
	_tests/test.sh

test-style:
	${DEV_ENV_CMD} shellcheck $(SHELL_SCRIPTS)

podman-build:
	# build the main image
	podman build --build-arg CODENAME=${CODENAME} -t ${IMAGE} .
	podman tag ${IMAGE} ${MUTABLE_IMAGE}

podman-buildx:
	podman buildx build --build-arg CODENAME=${CODENAME} --platform ${PLATFORM} -t ${IMAGE} . --push

deploy: build podman-build podman-push

.PHONY: all bootstrap build test podman-build deploy
