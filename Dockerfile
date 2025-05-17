# Stage 1: Build the Go binary
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the Go application
# Statically link to ensure the binary is self-contained for distroless/scratch images
# CGO_ENABLED=0 is crucial for static linking and small images
# -ldflags "-w -s" strips debug information and symbol table, reducing binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /kube-dethrottler cmd/kube-dethrottler/main.go

# Stage 2: Create the minimal final image
# Using distroless/static:nonroot for a secure, minimal image without a shell or unnecessary tools
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

# Copy the statically linked binary from the builder stage
COPY --from=builder /kube-dethrottler /app/kube-dethrottler

# Create a non-root user and group (distroless/static:nonroot already has a nonroot user)
# Ensure the binary is executable by this user
USER nonroot:nonroot

# (Optional) Copy default configuration file. This will be overridden by ConfigMap in Kubernetes.
# COPY config/default.yaml /etc/kube-dethrottler/config.yaml

# Set the entrypoint for the container
ENTRYPOINT ["/app/kube-dethrottler"]

# (Optional) Default command arguments (e.g., path to config file)
# CMD ["--config", "/etc/kube-dethrottler/config.yaml"] 