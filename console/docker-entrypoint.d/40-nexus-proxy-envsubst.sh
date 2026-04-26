#!/bin/sh
set -eu

envsubst '${NEXUS_PROXY_API_KEY} ${COMPANION_PROXY_API_KEY}' \
  < /etc/nginx/templates/default.conf.template \
  > /etc/nginx/conf.d/default.conf
