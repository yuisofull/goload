#!/bin/bash

# Script to generate protobuf files for goload project

set -e  # Exit on any error

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

# Get the project root directory (parent of scripts directory)
PROJECT_ROOT="$(pwd)"

# Create pb directories if they don't exist
print_info "Creating pb directories..."
mkdir -p internal/auth/pb
mkdir -p internal/task/pb
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
    --go_out=internal/task/pb \
    --go_opt=paths=source_relative \
    --go-grpc_out=internal/task/pb \
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

# Generate SQLC code
print_info "Generating SQLC code..."
sqlc generate -f configs/auth_svc_sqlc.yaml
sqlc generate -f configs/download_task_svc_sqlc.yaml
#sqlc generate -f configs/file_svc_sqlc.yaml