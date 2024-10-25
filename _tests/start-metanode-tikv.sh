#!/usr/bin/env bash
weed-start metanode tikv --data-dir=/data \
    --pd-endpoints="${DRYCC_STORAGE_TIPD_ENDPOINTS}" \
    --addr=0.0.0.0:20160 --status-addr=0.0.0.0:20180 \
    --advertise-addr="$(hostname -I | tr -d ' '):20160" \
    --advertise-status-addr="$(hostname -I | tr -d ' '):20180"
