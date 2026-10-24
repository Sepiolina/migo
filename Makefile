# Cross-platform builds (no cgo, single static binary)
build:
	go build -o miservice ./cmd/miservice
linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/miservice ./cmd/miservice
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/miservice.exe ./cmd/miservice
test:
	go vet ./...
	go test ./...
