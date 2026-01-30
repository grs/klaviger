# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:latest AS builder

# Copy source code
WORKDIR /opt/app-root/src
COPY . .

# Download dependencies
RUN go mod download

# Build the application
RUN go build -buildvcs=false -o klaviger ./cmd/klaviger/main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Install CA certificates
RUN microdnf install -y ca-certificates && \
    microdnf clean all

# Copy binary from builder
COPY --from=builder /opt/app-root/src/klaviger /usr/local/bin/klaviger

# Copy Kubernetes config example
COPY --from=builder /opt/app-root/src/examples/config-kubernetes.yaml /etc/klaviger/config.yaml

# Create non-root user
RUN useradd -u 1001 -r -g 0 -m -d /home/klaviger -s /sbin/nologin \
    -c "klaviger user" klaviger && \
    chown -R 1001:0 /etc/klaviger && \
    chmod -R g=u /etc/klaviger

# Use non-root user
USER 1001

# Expose ports
# Note: Forward proxy (8181) is not exposed as it's bound to 127.0.0.1 for pod-internal use only
EXPOSE 8180 8190

# Set default command
ENTRYPOINT ["/usr/local/bin/klaviger"]
CMD ["--config", "/etc/klaviger/config.yaml"]
