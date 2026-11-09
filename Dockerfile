FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /worker .

FROM scratch
COPY --from=build /worker /worker
USER 65532:65532
ENTRYPOINT ["/worker"]
