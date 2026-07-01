# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
#
# Runtime image for the Corral MCP server.
#
# The binary is expected to already be built by goreleaser and dropped at
# the build context root as `corralctl`. Do NOT compile from source in this
# Dockerfile — goreleaser's dockers: section produces one image per arch
# reusing the same statically-linked binary it publishes as a tar.gz.
#
# The `io.modelcontextprotocol.server.name` LABEL is the ownership marker
# the MCP registry uses to verify that
# https://ghcr.io/sebastienrousseau/corral belongs to the
# io.github.sebastienrousseau/corral server entry.
#
# Base image is pinned to a digest per OpenSSF Scorecard
# PinnedDependenciesID: an immutable reference protects the release
# supply chain from a poisoned `alpine:3.20` tag rotation. Update the
# digest when refreshing Alpine (e.g. moving to 3.21) or when the
# upstream image publishes a security fix.
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

# Runtime deps: git is required for clone/pull, ca-certificates for TLS.
RUN apk add --no-cache git ca-certificates \
    && addgroup -S corral \
    && adduser -S -G corral corral

COPY corralctl /usr/local/bin/corralctl

# OCI-standard labels for image indexing.
LABEL org.opencontainers.image.source="https://github.com/sebastienrousseau/corral" \
      org.opencontainers.image.description="Corral: local index for AI coding agents. MCP server exposes the Corral-organised workspace to Claude Code, Cursor, Cline, and other MCP clients." \
      org.opencontainers.image.licenses="GPL-3.0" \
      org.opencontainers.image.title="corral" \
      org.opencontainers.image.vendor="Sebastien Rousseau"

# MCP registry ownership label. MUST match the `name` field in server.json;
# the registry rejects publish attempts when they diverge.
LABEL io.modelcontextprotocol.server.name="io.github.sebastienrousseau/corral"

USER corral
WORKDIR /home/corral

# Default entrypoint runs the MCP server on stdio. Override with e.g.
# `docker run … corralctl <owner>` for the classic clone/sync workflow.
ENTRYPOINT ["/usr/local/bin/corralctl"]
CMD ["mcp"]
