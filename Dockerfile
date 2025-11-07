ARG CODENAME
FROM registry.drycc.cc/drycc/base:${CODENAME}

ARG DRYCC_UID=1001 \
  DRYCC_GID=1001 \
  DRYCC_HOME_DIR=/data \
  RUSTFS_VERSION="1.0.0-alpha.67" \
  OPENTELEMETRY_COLLECTOR_VERSION=0.139.0

COPY rootfs/etc/otelcol /etc/otelcol

RUN groupadd drycc --gid ${DRYCC_GID} \
  && useradd drycc -u ${DRYCC_UID} -g ${DRYCC_GID} -s /bin/bash -m -d ${DRYCC_HOME_DIR} \
  && install-packages dnsutils \
  && install-stack rustfs $RUSTFS_VERSION \
  && install-stack opentelemetry-collector $OPENTELEMETRY_COLLECTOR_VERSION \
  && rm -rf \
      /usr/share/doc \
      /usr/share/man \
      /usr/share/info \
      /usr/share/locale \
      /var/lib/apt/lists/* \
      /var/log/* \
      /var/cache/debconf/* \
      /etc/systemd \
      /lib/lsb \
      /lib/udev \
      /usr/lib/`echo $(uname -m)`-linux-gnu/gconv/IBM* \
      /usr/lib/`echo $(uname -m)`-linux-gnu/gconv/EBC* \
  && mkdir -p /usr/share/man/man{1..8}

USER ${DRYCC_UID}
