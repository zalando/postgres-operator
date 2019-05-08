FROM python:3.5-slim
MAINTAINER Team ACID @ Zalando <team-acid@zalando.de>

WORKDIR /e2e

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY tests .