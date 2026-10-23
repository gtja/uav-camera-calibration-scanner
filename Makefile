IMAGE_NAME ?= uav-camera-calibration-scanner
VERSION ?= $(shell cat VERSION 2>/dev/null || echo v0.1.0)
DATE_TAG ?= $(shell date +'%y%m%d')
REGISTRY ?= registry.jingan.com:32008/ja
PUBLIC_REGISTRY ?= registry.cn-hangzhou.aliyuncs.com/jingansi
IMAGE_TAG ?= $(VERSION).$(DATE_TAG)
AMD_IMAGE ?= $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
ARM_IMAGE ?= $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)-arm
PUBLIC_AMD_IMAGE ?= $(PUBLIC_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
PUBLIC_ARM_IMAGE ?= $(PUBLIC_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)-arm
IMAGE ?= $(AMD_IMAGE)
DOCKER_PLATFORM ?= linux/amd64
CONFIG ?= configs/config.yml
DATE ?=
INTEGRATION_CONFIG ?= configs/config.example.json
INTEGRATION_DATE ?= 2026-07-06
INTEGRATION_ARGS ?= -dry-run
HELM_RELEASE ?= uav-camera-calibration-scanner
HELM_NAMESPACE ?= ja
HELM_CHART ?= deploy/helm/uav-camera-calibration-scanner
HELM_VALUES ?= $(HELM_CHART)/values-test.yaml

.PHONY: test integration-test release-checklist build docker-build docker-build-amd docker-build-arm docker-build-public docker-build-public-arm docker-push docker-push-amd docker-push-arm docker-push-public docker-push-public-arm docker-save docker-run-once docker-up docker-down docker-logs helm-lint helm-template helm-upgrade helm-uninstall release print-image

test:
	go test ./...

integration-test:
	go run ./cmd/uav-camera-calibration-scanner -config $(INTEGRATION_CONFIG) -mode once -date $(INTEGRATION_DATE) $(INTEGRATION_ARGS)

release-checklist: test integration-test
	@echo "release checklist passed: unit tests and integration test"

build:
	CGO_ENABLED=0 go build -buildvcs=false -o bin/uav-camera-calibration-scanner ./cmd/uav-camera-calibration-scanner

docker-build:
	docker build --platform $(DOCKER_PLATFORM) -t $(IMAGE) .

docker-build-amd:
	docker build -f Dockerfile -t $(AMD_IMAGE) --platform=linux/amd64 .

docker-build-arm:
	docker build -f Dockerfile -t $(ARM_IMAGE) --platform=linux/arm64 .

docker-build-public:
	docker build -f Dockerfile -t $(PUBLIC_AMD_IMAGE) --platform=linux/amd64 .

docker-build-public-arm:
	docker build -f Dockerfile -t $(PUBLIC_ARM_IMAGE) --platform=linux/arm64 .

docker-push: docker-build
	docker push $(IMAGE)

docker-push-amd:
	docker push $(AMD_IMAGE)

docker-push-arm:
	docker push $(ARM_IMAGE)

docker-push-public:
	docker push $(PUBLIC_AMD_IMAGE)

docker-push-public-arm:
	docker push $(PUBLIC_ARM_IMAGE)

docker-save:
	mkdir -p out/release
	docker save $(IMAGE) > out/release/$(IMAGE_NAME)-$(IMAGE_TAG).tar

release: release-checklist docker-build

docker-run-once:
	docker run --rm \
		-v $(PWD)/$(CONFIG):/config/config.yml:ro \
		-v $(PWD)/out/docker:/app/out/docker \
		$(IMAGE) -config /config/config.yml -mode once $(if $(DATE),-date $(DATE),)

docker-up:
	IMAGE=$(IMAGE) CONFIG=$(CONFIG) docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f --tail=200 uav-camera-calibration-scanner

helm-lint:
	helm lint $(HELM_CHART) -f $(HELM_VALUES) --set image.tag=$(IMAGE_TAG)

helm-template:
	helm template $(HELM_RELEASE) $(HELM_CHART) -n $(HELM_NAMESPACE) -f $(HELM_VALUES) --set image.tag=$(IMAGE_TAG)

helm-upgrade:
	helm upgrade --install $(HELM_RELEASE) $(HELM_CHART) -n $(HELM_NAMESPACE) --create-namespace -f $(HELM_VALUES) --set image.tag=$(IMAGE_TAG)

helm-uninstall:
	helm uninstall $(HELM_RELEASE) -n $(HELM_NAMESPACE)

print-image:
	@echo "VERSION=$(VERSION)"
	@echo "IMAGE_TAG=$(IMAGE_TAG)"
	@echo "AMD_IMAGE=$(AMD_IMAGE)"
	@echo "ARM_IMAGE=$(ARM_IMAGE)"
	@echo "PUBLIC_AMD_IMAGE=$(PUBLIC_AMD_IMAGE)"
	@echo "PUBLIC_ARM_IMAGE=$(PUBLIC_ARM_IMAGE)"
