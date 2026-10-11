# agentbox pi adapter image: base + a pinned pi npm package (FR-10).
# Installed with --ignore-scripts so package lifecycle hooks never run at
# build time (NFR-8 / AC 16).
ARG BASE_IMAGE=agentbox-base:dev
FROM ${BASE_IMAGE}

ARG PI_VERSION=1.1.0
RUN set -eux; \
	npm install -g --ignore-scripts "@earendil-works/pi-coding-agent@${PI_VERSION}"; \
	npm cache clean --force; \
	rm -rf /root/.npm /tmp/*; \
	find /usr/local/lib/node_modules/@earendil-works -name '*.map' -delete; \
	find /usr/local/lib/node_modules/@earendil-works -name '*.d.ts' -delete; \
	find /usr/local/lib/node_modules/@earendil-works -type d \
		\( -name docs -o -name examples \) -prune -exec rm -rf {} +; \
	find /usr/local/lib/node_modules/@earendil-works -name '*.md' -delete

CMD ["pi"]
