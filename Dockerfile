FROM golang:1.26.11-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=secret,id=proxy_ca --mount=type=tmpfs,target=/run/go-trust \
    if [ -f /run/secrets/proxy_ca ]; then \
        cat /etc/ssl/certs/ca-certificates.crt /run/secrets/proxy_ca > /run/go-trust/ca-bundle.crt && \
        export SSL_CERT_FILE=/run/go-trust/ca-bundle.crt; \
    fi && \
    go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mcmods-cn-backend .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/mcmods-cn-backend /app/mcmods-cn-backend
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/mcmods-cn-backend"]
