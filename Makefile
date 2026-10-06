BINARY=dvremove
SERVER=192.168.1.3
SERVER_PORT=9999
DEPLOY_DIR=/home/chperso/dvremove
BUILD_DIR=$(DEPLOY_DIR)/build
UNIT_DIR=.config/systemd/user

.PHONY: build build-linux test runbooks deploy image install-units clean

build:
	go build -o $(BINARY) .

# A static binary for the image. The VCS stamp stays, so `go version -m` reads the head.
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o $(BINARY) .

test:
	go test -race -count=1 ./...
	deploy/linux-lab-01/run_test.sh
	deploy/linux-lab-01/runbooks_test.sh

# Runs every step of every runbook on the host. A step that changes state runs against a fixture the runner plants and removes.
runbooks:
	RB_HOST=$(SERVER) RB_PORT=$(SERVER_PORT) RB_ROOT=/home/chperso/dvremove-test deploy/linux-lab-01/runbooks.sh

# Copies the binary, Dockerfile, .dockerignore and launcher files to the build folder, and nothing else.
deploy: build-linux
	ssh -p $(SERVER_PORT) $(SERVER) "mkdir -p $(BUILD_DIR)/deploy"
	scp -P $(SERVER_PORT) $(BINARY) Dockerfile .dockerignore $(SERVER):$(BUILD_DIR)/
	scp -r -P $(SERVER_PORT) deploy/linux-lab-01 $(SERVER):$(BUILD_DIR)/deploy/
	@echo "Deployed to $(SERVER):$(BUILD_DIR)"

# Builds dvremove:<head> on the host from the build folder and tags it dvremove:current.
image:
	@test -z "$$(git status --porcelain)" || { echo "image: the working tree is dirty; commit or stash first" >&2; exit 1; }
	head=$$(git rev-parse --short HEAD) && \
	ssh -p $(SERVER_PORT) $(SERVER) "cd $(BUILD_DIR) && docker build -t dvremove:$$head . && docker tag dvremove:$$head dvremove:current"

install-units:
	ssh -p $(SERVER_PORT) $(SERVER) "mkdir -p $(UNIT_DIR)"
	scp -P $(SERVER_PORT) deploy/linux-lab-01/dvremove.service deploy/linux-lab-01/dvremove.timer $(SERVER):$(UNIT_DIR)/

clean:
	rm -f $(BINARY)
