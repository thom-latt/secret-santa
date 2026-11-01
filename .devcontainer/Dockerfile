FROM docker.io/library/golang:1.27

# Node 25 (same major as the production build stage)
COPY --from=docker.io/library/node:25-slim /usr/local/bin/node /usr/local/bin/node
COPY --from=docker.io/library/node:25-slim /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && apt-get update && apt-get install -y --no-install-recommends make git sqlite3 \
 && rm -rf /var/lib/apt/lists/*

# Non-root user (uid 1000 matches the host user; Podman uses --userns=keep-id)
ARG USERNAME=dev
ARG UID=1000
RUN useradd -m -u ${UID} -s /bin/bash ${USERNAME} \
 && mkdir -p /home/${USERNAME}/go && chown -R ${UID}:${UID} /home/${USERNAME}
ENV GOPATH=/home/${USERNAME}/go \
    PATH=/home/${USERNAME}/go/bin:${PATH}
USER ${USERNAME}
RUN go install golang.org/x/tools/gopls@latest
