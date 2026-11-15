# The news desk's n8n for hosts (Render): the workflow ships inside the image
# and is imported and published every time the container starts, so a
# redeploy or a wiped disk never loses it. docker-compose uses the stock
# image and mounts ./n8n instead.
FROM docker.n8n.io/n8nio/n8n:2.46.6

COPY --chown=node:node autonews-agent.json /opt/autonews-agent.json
COPY --chown=node:node start.sh /opt/start.sh

ENTRYPOINT ["/sbin/tini", "--", "sh", "/opt/start.sh"]
