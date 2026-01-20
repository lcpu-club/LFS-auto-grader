all: build

build-utility:
    go build -o ./build/utility ./cmd/utility

build-manager:
    CGO_ENABLED=0 go build -o ./build/manager ./cmd/manager

build: build-utility build-manager

build-image:
    docker build . -t lfs-auto-grader:latest

run:
    ./build/manager -redis-config="redis://localhost:6379" -endpoint="https://hpcgame.pku.edu.cn"

test:
    go test ./...
