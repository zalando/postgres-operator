# An image to run e2e tests.
# The image does not include the tests; all necessary files are bind-mounted when a container starts.
FROM python:3.11-slim
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

ENV TERM xterm-256color

RUN apt-get -qq -y update \
 # https://www.psycopg.org/docs/install.html#psycopg-vs-psycopg-binary
    && apt-get -qq -y install --no-install-recommends curl vim python3-dev \
    && curl -LO https://dl.k8s.io/release/v1.32.9/bin/linux/amd64/kubectl \
    && chmod +x ./kubectl \
    && mv ./kubectl /usr/local/bin/kubectl \
    && apt-get -qq -y clean \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt ./
RUN pip install -r ./requirements.txt

CMD ["python", "-m", "unittest", "discover", "-v", "--failfast", "--start-directory", "/tests"]