#!/usr/bin/env bash

BASE_DIR=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
MINIO_ROOT_USER=f4c4281665bc11ee8e0400163e04a9cd
MINIO_ROOT_PASSWORD=f4c4281665bc11ee8e0400163e04a9cd

function start-storage {
  mkdir -p "${BASE_DIR}/data"
  podman run --rm -d --name test-storage \
    -e MINIO_PROMETHEUS_AUTH_TYPE=public \
    -e MINIO_ROOT_USER=${MINIO_ROOT_USER} \
    -e MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD} \
    registry.drycc.cc/drycc/storage:canary \
    minio server /tmp --address :9000 --console-address :9001
}

# shellcheck disable=SC2317
function clean_before_exit {
  # delay before exiting, so stdout/stderr flushes through the logging system
  podman kill test-storage
  rm -rf "${BASE_DIR}/data"
}
trap clean_before_exit EXIT

function main {
  start-storage
  S3_IP=$(podman inspect --format "{{ .NetworkSettings.IPAddress }}" test-storage)
  S3_ENDPOINT=http://${S3_IP}:9000
  # wait for port
  echo -e "\\033[32m---> Waitting for ${S3_IP}:9000\\033[0m"
  wait-for-port --host="${S3_IP}" 9000
  echo -e "\\033[32m---> S3 service ${S3_IP}:9000 ready...\\033[0m"
  # test by rclone client
  mkdir -p /tmp/.config/rclone
  cat > /tmp/.config/rclone/rclone.conf << EOF
[storage]
type = s3
provider = Other
endpoint = ${S3_ENDPOINT}
access_key_id = ${MINIO_ROOT_USER}
secret_access_key = ${MINIO_ROOT_PASSWORD}
EOF
  rclone --config /tmp/.config/rclone/rclone.conf mkdir storage:test
  rclone --config /tmp/.config/rclone/rclone.conf copyto "${BASE_DIR}"/test.sh storage:test/test.sh
  exit_code=$?
  rm -rf /tmp/.config/rclone
  exit $exit_code
}

main