# builder stage
FROM golang:1.24-alpine AS builder

# Create an unprivileged user for security
RUN adduser -D -g '' cacheuser

WORKDIR /app

# Copy dependency files first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary statically with extreme optimizations
# - CGO_ENABLED=0: Statically links the binary
# - trimpath: Removes absolute file system paths from the compiled executable
# - ldflags="-s -w": Strips debugging information and symbol tables to reduce size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o kv-cache .

# final production image
# Use 'scratch', an empty image, for the smallest possible footprint
FROM scratch

# Copy the user/group information from the builder stage
COPY --from=builder /etc/passwd /etc/passwd

# Copy the statically compiled binary
COPY --from=builder /app/kv-cache /kv-cache

# Tell Docker to run the container as the unprivileged user
USER cacheuser

# Expose the application port
EXPOSE 7171

# Use ENTRYPOINT instead of CMD for pure executables
ENTRYPOINT ["/kv-cache"]