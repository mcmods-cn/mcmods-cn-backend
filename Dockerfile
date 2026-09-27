FROM golang:1.26.6-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mcmods-cn-backend .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/mcmods-cn-backend /app/mcmods-cn-backend
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/mcmods-cn-backend"]
