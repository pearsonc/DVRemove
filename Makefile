BINARY=dvremove
SERVER=192.168.1.3
SERVER_PORT=9999
DEPLOY_DIR=/home/chperso/dvremove

.PHONY: build build-linux deploy clean

build:
	go build -o $(BINARY) .

build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY) .

deploy: build-linux
	ssh -p $(SERVER_PORT) $(SERVER) "mkdir -p $(DEPLOY_DIR)/{logs,toConvert,Converted}"
	scp -P $(SERVER_PORT) $(BINARY) config.yaml $(SERVER):$(DEPLOY_DIR)/
	@echo "Deployed to $(SERVER):$(DEPLOY_DIR)"

clean:
	rm -f $(BINARY)
