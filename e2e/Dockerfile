FROM ubuntu:18.04
MAINTAINER Team ACID @ Zalando <team-acid@zalando.de>

WORKDIR /e2e

COPY e2e/requirements.txt ./
COPY e2e/manifests ./manifests
COPY e2e/tests ./tests
COPY e2e/kind-config-kind-smoke-test-postgres-operator /root/.kube/config

RUN apt-get update
RUN apt-get install -y python3 python3-pip curl \
    && pip3 install --no-cache-dir -r requirements.txt
RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.14.0/bin/linux/amd64/kubectl \
    && chmod +x ./kubectl \
    && mv ./kubectl /usr/local/bin/kubectl

CMD ["python3", "-m", "unittest", "discover", "--start-directory", "tests", "-v"]