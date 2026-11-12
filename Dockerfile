# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0

COPY go.mod ./
COPY . .
RUN go test ./... \
    && go build -trimpath -ldflags="-s -w" -o /out/tour-track .

# The application embeds its templates, static assets, and demo snapshot.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/tour-track /tour-track
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/tour-track"]
