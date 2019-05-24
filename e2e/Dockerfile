FROM ubuntu:18.04
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

WORKDIR /e2e

COPY manifests ./manifests
COPY e2e/requirements.txt e2e/tests ./

RUN apt-get update \
    && apt-get install --no-install-recommends -y \ 
           python3=3.6.7-1~18.04 \
           python3-setuptools=39.0.1-2 \
           python3-pip=9.0.1-2.3~ubuntu1 \
           curl=7.58.0-2ubuntu3.7 \
    && pip3 install --no-cache-dir -r requirements.txt \
    && curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.14.0/bin/linux/amd64/kubectl \
    && chmod +x ./kubectl \
    && mv ./kubectl /usr/local/bin/kubectl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

CMD ["python3", "-m", "unittest", "discover", "--start-directory", ".", "-v"]