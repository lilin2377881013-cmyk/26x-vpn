FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/26x-api ./cmd/26x-api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/26x-api /app/26x-api
COPY migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/26x-api"]
