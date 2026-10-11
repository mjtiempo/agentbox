# agentbox base image: node:24-bookworm-slim pinned by digest + the tooling
# the entrypoint and shell adapter need (FR-10). Target size <= 400 MB.
#
# Why two stages: Docker layers are additive, so stripping node or deleting
# files in a child of the official image would *add* a second copy instead of
# shrinking the image. Stage 1 prepares a slim /usr/local; stage 2 copies it
# into a plain debian:bookworm-slim, which keeps the runtime at one copy of
# every file.
FROM node:24-bookworm-slim@sha256:d6aa754f16b3197301076f047b5def2f02ea1dbbc2ca920407d46d7ec7f87b20 AS node-dist

RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends binutils; \
	strip --strip-all /usr/local/bin/node; \
	rm -rf /usr/local/include /usr/local/CHANGELOG.md /usr/local/LICENSE /usr/local/README.md; \
	rm -rf /opt/yarn* /usr/local/lib/node_modules/corepack /usr/local/lib/node_modules/yarn*; \
	rm -f /usr/local/bin/corepack /usr/local/bin/yarn /usr/local/bin/yarnpkg; \
	find /usr/local/lib/node_modules/npm -name '*.md' -delete; \
	find /usr/local/lib/node_modules/npm -name '*.map' -delete; \
	find /usr/local/lib/node_modules/npm -name '*.d.ts' -delete; \
	rm -rf /var/lib/apt/lists/*

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587

RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends \
		bash ca-certificates git ripgrep util-linux libstdc++6; \
	rm -rf /var/lib/apt/lists/* /usr/share/doc/* /usr/share/man/*

# node + npm, already stripped/pruned by stage 1. npm stays so agents can run
# project installs; git keeps perl because git-add--interactive needs it.
COPY --from=node-dist /usr/local /usr/local

# Single source of truth: the same script Go embeds via go:embed.
COPY internal/entrypoint/entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh

WORKDIR /workspace
CMD ["bash"]
