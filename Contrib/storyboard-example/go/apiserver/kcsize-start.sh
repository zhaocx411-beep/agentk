#!/bin/sh
set -eu

: "${AUTO_PORT0:?AUTO_PORT0 is required}"
export API_PORT="${AUTO_PORT0}"

cd /home/web_server/storyboard-example-apiserver
exec /usr/local/bin/storyboard-example-apiserver
