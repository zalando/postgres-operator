FROM ubuntu:18.04
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

WORKDIR /e2e

COPY manifests ./manifests
COPY e2e/requirements.txt e2e/tests ./

RUN apt-get update \
    && apt-get install -y python3 python3-pip curl \
    && pip3 install --no-cache-dir -r requirements.txt \
    && curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.14.0/bin/linux/amd64/kubectl \
    && chmod +x ./kubectl \
    && mv ./kubectl /usr/local/bin/kubectl

CMD ["python3", "-m", "unittest", "discover", "--start-directory", ".", "-v"]