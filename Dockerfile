FROM alpine
MAINTAINER Team ACID @ Zalando <team-acid@zalando.de>

ADD build/linux/postgres-operator /postgres-operator
ADD scm-source.json /

ENTRYPOINT ["/postres-operator"]

