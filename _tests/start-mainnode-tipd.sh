#!/usr/bin/env bash
DRYCC_STORAGE_TIPD_ENDPOINTS="http://$(hostname -I | tr -d ' '):2379"
export DRYCC_STORAGE_TIPD_ENDPOINTS
weed-start mainnode tipd --name=drycc-storage-mainnode --data-dir=/data \
    --client-urls=http://0.0.0.0:2379 --peer-urls=http://0.0.0.0:2380 \
    --advertise-client-urls="http://$(hostname -I | tr -d ' '):2379" \
    --advertise-peer-urls="http://$(hostname -I | tr -d ' '):2380"
