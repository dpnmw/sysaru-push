# --- build ---
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /sysaru-push .

# --- run ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /sysaru-push /sysaru-push
EXPOSE 8080
ENTRYPOINT ["/sysaru-push"]
