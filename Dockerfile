# syntax=docker/dockerfile:1

# --- Stage 1: build the React SPA -------------------------------------------
FROM node:26-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# Stamped into the UI for feedback links; deploys pass the commit hash.
ARG APP_VERSION=dev
# Where feedback links point; empty means the upstream repo.
ARG ISSUES_URL=
ENV VITE_APP_VERSION=$APP_VERSION VITE_ISSUES_URL=$ISSUES_URL
RUN npm run build

# --- Stage 2: build the static Go binary ------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
# Cache module downloads first.
COPY go.mod go.sum ./
RUN go mod download
# Copy the rest of the source.
COPY . .
# Overwrite the committed placeholder with the freshly built SPA so it is
# embedded by //go:embed.
RUN rm -rf webembed/dist
COPY --from=web /web/dist ./webembed/dist
# Fail the build if tests fail, then produce a static CGO-free binary.
RUN go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /quickvote ./cmd/quickvote

# --- Stage 3: minimal runtime image -----------------------------------------
FROM alpine:3.24
RUN adduser -D -u 10001 quickvote
COPY --from=build /quickvote /quickvote
RUN mkdir -p /data && chown quickvote:quickvote /data
USER quickvote
VOLUME /data
EXPOSE 8080
ENV QV_ADDR=:8080 QV_DB=/data/quickvote.db
ENTRYPOINT ["/quickvote"]
