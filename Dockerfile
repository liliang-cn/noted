FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/noted ./cmd/noted

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/noted /noted
ENV NOTED_LISTEN=0.0.0.0:43872 NOTED_DATA_DIR=/data
VOLUME /data
EXPOSE 43872
HEALTHCHECK --interval=30s --timeout=5s CMD ["/noted", "health", "-addr", "127.0.0.1:43872"]
ENTRYPOINT ["/noted"]
CMD ["serve"]
