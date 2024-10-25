#!/usr/bin/env bash
eval "cat <<EOF >/etc/seaweedfs/s3.json
$( cat /usr/local/bin/s3.json )
EOF
" 2> /dev/null

cat << EOF > "/etc/seaweedfs/filer.toml"
[tikv]
enabled = true
pdaddrs = "${DRYCC_STORAGE_TIPD_ENDPOINTS}"
deleterange_concurrency = 30
enable_1pc = true
EOF

weed-start metanode weed -master="${MASTER}" -defaultStoreDir=/data -metricsPort=9326
