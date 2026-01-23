.PHONY: build push clean help

IMAGE_NAME ?= kube-state-logs
IMAGE_TAG ?= latest
REGISTRY ?= ghcr.io/azure/

# Build Docker image
build:
	docker build -t $(REGISTRY)$(IMAGE_NAME):$(IMAGE_TAG) .

# Build for multiple platforms
build-multi:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(REGISTRY)$(IMAGE_NAME):$(IMAGE_TAG) .

# Push Docker image
push:
	docker push $(REGISTRY)$(IMAGE_NAME):$(IMAGE_TAG)

# Clean local images
clean:
	docker rmi $(REGISTRY)$(IMAGE_NAME):$(IMAGE_TAG) || true

# Show help
help:
	@echo "Available targets:"
	@echo "  build       - Build Docker image"
	@echo "  build-multi - Build Docker image for multiple platforms"
	@echo "  push        - Push Docker image"
	@echo "  clean       - Remove local Docker image"
	@echo ""
	@echo "Variables:"
	@echo "  IMAGE_NAME  - Image name (default: kube-state-logs)"
	@echo "  IMAGE_TAG   - Image tag (default: latest)"
	@echo "  REGISTRY    - Registry prefix (default: ghcr.io/azure/)"
 