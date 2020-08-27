# An image to perform the actual test. Do not forget to copy all necessary test
# files here.
FROM ubuntu:18.04
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

COPY manifests ./manifests
COPY exec.sh ./exec.sh
COPY requirements.txt tests ./

RUN apt-get update \
    && apt-get install --no-install-recommends -y \
           python3 \
           python3-setuptools \
           python3-pip \
           curl \
    && pip3 install --no-cache-dir -r requirements.txt \
    && curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.14.0/bin/linux/amd64/kubectl \
    && chmod +x ./kubectl \
    && mv ./kubectl /usr/local/bin/kubectl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

ARG VERSION=dev
RUN sed -i "s/__version__ = .*/__version__ = '${VERSION}'/" ./__init__.py

CMD ["python3", "-m", "unittest", "discover", "--start-directory", ".", "-v"]
