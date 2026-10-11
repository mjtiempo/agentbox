# agentbox opencode adapter image: base + the pinned opencode v2 CLI.
#
# opencode v2 is published as @opencode/cli (opencode-ai remains the 1.x
# line); the v2 wrapper ships a native binary in optional platform packages
# plus a postinstall that hardlinks the matching one into bin/opencode.exe.
#
# Install hygiene (FR-10/AC 16): npm install runs with --ignore-scripts. The
# vendor's postinstall.mjs (platform/AVX2/musl detection + hardlink) is run
# explicitly below, then every platform package is removed — the wrapper keeps
# the binary via hardlink, and the binary resolves its version from this
# package path.
#
# Size note: the v2 native binary is ~210 MB. The FR-10 400 MB target is met
# by base/pi; opencode exceeds it because of the upstream binary size —
# recorded in the release manifest (NFR-8).
ARG BASE_IMAGE=agentbox-base:dev
FROM ${BASE_IMAGE}

ARG OPENCODE_VERSION=2.0.26
RUN set -eux; \
	npm install -g --ignore-scripts "@opencode/cli@${OPENCODE_VERSION}"; \
	npm cache clean --force; \
	rm -rf /root/.npm /tmp/*; \
	OPENCODE_DIR=/usr/local/lib/node_modules/@opencode/cli; \
	node "${OPENCODE_DIR}/postinstall.mjs"; \
	rm -rf "${OPENCODE_DIR}/node_modules/@opencode"; \
	find "${OPENCODE_DIR}" -name '*.map' -delete; \
	find "${OPENCODE_DIR}" -name '*.md' -delete; \
	find "${OPENCODE_DIR}" -name '*.d.ts' -delete

CMD ["opencode"]
