docker build -t ubuntu-xfce-novnc . && \
docker export $(docker create ubuntu-xfce-novnc) | tar -C /srv/overlays/base -x
