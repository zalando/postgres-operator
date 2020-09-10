# An image to run e2e tests.
# The image does not include the tests; all necessary files are bind-mounted when a container starts.
FROM ubuntu:20.04
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

ENV TERM xterm-256color

COPY requirements.txt ./
COPY scm-source.json ./

RUN apt-get update \
    && apt-get install --no-install-recommends -y \
           python3 \
           python3-setuptools \
           python3-pip \
           curl \
    && pip3 install --no-cache-dir -r requirements.txt \
    && curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.18.0/bin/linux/amd64/kubectl \
    && chmod +x ./kubectl \
    && mv ./kubectl /usr/local/bin/kubectl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

ENTRYPOINT ["python3", "-m", "unittest", "discover", "--start-directory", ".", "-v"]
