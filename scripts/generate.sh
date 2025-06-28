#!/bin/bash

# Script to generate protobuf files for goload project
# This script generates Go code from .proto files in api/ directory
# and places them in the corresponding internal/{module}/pb/ directories

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if protoc is installed
if ! command -v protoc &> /dev/null; then
    print_error "protoc is not installed. Please install Protocol Buffers compiler."
    print_info "Visit: https://grpc.io/docs/protoc-installation/"
    exit 1
fi

# Check if protoc-gen-go is installed
if ! command -v protoc-gen-go &> /dev/null; then
    print_error "protoc-gen-go is not installed."
    print_info "Install it with: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    exit 1
fi

# Check if protoc-gen-go-grpc is installed
if ! command -v protoc-gen-go-grpc &> /dev/null; then
    print_error "protoc-gen-go-grpc is not installed."
    print_info "Install it with: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    exit 1
fi

print_info "Starting protobuf generation..."

# Get the project root directory (parent of scripts directory)
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

# Create pb directories if they don't exist
print_info "Creating pb directories..."
mkdir -p internal/auth/pb
mkdir -p internal/downloadtask/pb
mkdir -p internal/file/pb

# Generate protobuf files
print_info "Generating protobuf files..."

# Generate auth.proto
print_info "Generating auth.proto..."
protoc \
    --go_out=internal/auth/pb \
    --go_opt=paths=source_relative \
    --go-grpc_out=internal/auth/pb  \
    --go-grpc_opt=paths=source_relative \
    --proto_path=api \
    api/auth.proto

# Generate download_task.proto
print_info "Generating download_task.proto..."
protoc \
    --go_out=internal/downloadtask/pb \
    --go_opt=paths=source_relative \
    --go-grpc_out=internal/downloadtask/pb \
    --go-grpc_opt=paths=source_relative \
    --proto_path=api \
    api/download_task.proto

# Generate file.proto
print_info "Generating file.proto..."
protoc \
    --go_out=internal/file/pb \
    --go_opt=paths=source_relative \
    --go-grpc_out=internal/file/pb \
    --go-grpc_opt=paths=source_relative \
    --proto_path=api \
    api/file.proto

print_info "Protobuf generation completed successfully!"
print_info "Generated files:"
print_info "  - internal/auth/pb/"
print_info "  - internal/downloadtask/pb/"
print_info "  - internal/file/pb/"