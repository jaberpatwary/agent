#!/bin/sh
# Import the bundled workflow under its fixed id, publish it, then run n8n.
set -e

WORKFLOW_ID=BSbtu0j907cYvBvk

node -e "
const fs = require('fs');
const w = JSON.parse(fs.readFileSync('/opt/autonews-agent.json', 'utf8'));
w.id = '$WORKFLOW_ID';
fs.writeFileSync('/tmp/autonews-agent.json', JSON.stringify(w));
"

n8n import:workflow --input=/tmp/autonews-agent.json
n8n publish:workflow --id="$WORKFLOW_ID"

exec n8n start
