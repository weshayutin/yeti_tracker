FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOARCH=${TARGETARCH} go build -o /yeti_tracker .

FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /yeti_tracker .
COPY --from=build /src/templates ./templates
COPY --from=build /src/static ./static

EXPOSE 8080
ENTRYPOINT ["./yeti_tracker"]
