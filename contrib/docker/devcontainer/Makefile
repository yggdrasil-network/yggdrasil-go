# Development container management

.PHONY: dev-build dev-run dev-shell dev-stop dev-clean

# Build development container
dev-build:
	docker build -f Dockerfile -t yggdrasil-dev .

# Run development container with volume mounts
dev-run:
	docker run -it --rm \
		--name yggdrasil-dev \
		-v $(PWD):/workspace \
		-v ~/.gitconfig:/home/vscode/.gitconfig:ro \
		-p 9001:9001 \
		-p 9002:9002 \
		-p 9003:9003 \
		--privileged \
		--cap-add=NET_ADMIN \
		yggdrasil-dev

# Get shell access to running container
dev-shell:
	docker exec -it yggdrasil-dev /bin/zsh

# Stop development container
dev-stop:
	docker stop yggdrasil-dev || true

# Clean development artifacts
dev-clean:
	docker rmi yggdrasil-dev || true
	docker system prune -f

# Build and run in one command
dev: dev-build dev-run 