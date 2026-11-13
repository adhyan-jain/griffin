# The Griffin API server. Build: docker build -t griffin-server .
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/griffin-server ./cmd/griffin-server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/griffin-migrate ./cmd/griffin-migrate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/griffin-server /out/griffin-migrate /
EXPOSE 8080
ENTRYPOINT ["/griffin-server"]
