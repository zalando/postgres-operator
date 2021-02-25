FROM alpine:3.12.1
RUN apk add --no-cache curl;
RUN wget https://get.helm.sh/helm-v3.5.0-linux-amd64.tar.gz -O /root/helm.tar.gz; \
    tar -xzf /root/helm.tar.gz -C /root; \
    cp /root/linux-amd64/helm /usr/local/bin; \
    rm -rf /root/*
