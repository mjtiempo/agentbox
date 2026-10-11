# agentbox claude adapter image: base + a pinned Claude Code npm package.
#
# Install hygiene (FR-10/AC 16): npm install runs with --ignore-scripts so no
# package lifecycle hook fires implicitly during install. Claude Code ships a
# native binary in an optional platform package plus a small wrapper that
# hardlinks it into bin/claude.exe; that documented vendor step (install.cjs)
# is run explicitly below, never as an implicit postinstall.
#
# Size note: the upstream native binary is ~257 MB and already stripped. The
# FR-10 400 MB target is met by base/pi; claude exceeds it because of the
# upstream binary size — recorded in the release manifest (NFR-8).
ARG BASE_IMAGE=agentbox-base:dev
FROM ${BASE_IMAGE}

ARG CLAUDE_VERSION=2.1.296
RUN set -eux; \
	npm install -g --ignore-scripts "@anthropic-ai/claude-code@${CLAUDE_VERSION}"; \
	npm cache clean --force; \
	rm -rf /root/.npm /tmp/*; \
	CLAUDE_DIR=/usr/local/lib/node_modules/@anthropic-ai/claude-code; \
	node "${CLAUDE_DIR}/install.cjs"; \
	rm -rf "${CLAUDE_DIR}/node_modules/@anthropic-ai"/claude-code-*; \
	find "${CLAUDE_DIR}" -name '*.map' -delete; \
	find "${CLAUDE_DIR}" -name '*.md' -delete; \
	find "${CLAUDE_DIR}" -name '*.d.ts' -delete

CMD ["claude"]
