.PHONY: all build proto test clean docker

all: proto build

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	@if ! command -v protoc &> /dev/null; then \
		echo "Error: protoc is not installed. Please install Protocol Buffers compiler."; \
		echo "On macOS: brew install protobuf"; \
		echo "On Ubuntu/Debian: apt-get install protobuf-compiler"; \
		exit 1; \
	fi
	@./scripts/generate-proto.sh

# Build the application
build:
	@echo "Building application..."
	cd src && go build -o ../clamav-api main.go grpc_server.go

# Run tests
test:
	@echo "Running unit tests..."
	cd src && go test -v -short ./...

# Run all tests including integration tests
test-all:
	@echo "Running all tests (including integration)..."
	cd src && go test -v ./...

# Run integration tests only
test-integration:
	@echo "Running integration tests..."
	cd src && go test -v -tags=integration ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	cd src && go test -v -short -coverprofile=coverage.out ./...
	cd src && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: src/coverage.html"

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	cd src && go test -bench=. -benchmem ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f clamav-api
	rm -f proto/*.pb.go

# Build Docker image
docker:
	@echo "Building Docker image..."
	docker build -t clamav-api:latest .

# Install dependencies
deps:
	@echo "Installing Go dependencies..."
	cd src && go mod download
	cd src && go get google.golang.org/grpc@latest
	cd src && go get google.golang.org/protobuf@latest
	@echo "Installing protoc plugins..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Run the application
run: build
	@echo "Starting application..."
	./clamav-api

# Development mode (without gRPC for testing)
run-rest:
	@echo "Starting REST API only..."
	cd src && go run main.go -enable-grpc=false

help:
	@echo "Available targets:"
	@echo "  make all              - Generate proto files and build"
	@echo "  make proto            - Generate protobuf code"
	@echo "  make build            - Build the application"
	@echo "  make test             - Run unit tests"
	@echo "  make test-all         - Run all tests including integration"
	@echo "  make test-integration - Run integration tests only"
	@echo "  make test-coverage    - Run tests with coverage report"
	@echo "  make bench            - Run benchmarks"
	@echo "  make clean            - Clean build artifacts"
	@echo "  make docker           - Build Docker image"
	@echo "  make deps             - Install dependencies"
	@echo "  make run              - Build and run the application"
	@echo "  make run-rest         - Run REST API only (no gRPC)"
	@echo "  make help             - Show this help message"

